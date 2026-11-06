package dev.gofastr.nativeui;

import android.Manifest;
import android.app.*;
import android.content.*;
import android.content.pm.PackageManager;
import android.database.Cursor;
import android.media.MediaRecorder;
import android.net.Uri;
import android.os.Build;
import android.provider.MediaStore;
import android.provider.OpenableColumns;
import android.util.Base64;
import android.util.Log;
import android.widget.*;
import org.json.*;
import java.io.*;
import java.text.SimpleDateFormat;
import java.util.*;
import java.util.function.Consumer;

/** One foreground device operation, settled once through the Go-owned task ID. */
public final class Services {
    private final GofastrActivity host;
    private Consumer<JSONObject> result;
    private Consumer<Uri> picked;
    private Runnable permissionGranted;
    private int activityCode;
    private File cameraFile,recordingFile;
    private MediaRecorder recorder;
    private AlertDialog recordingDialog;
    private Runnable recordingLimit;
    private long recordingStarted;
    private int maximumDuration,maximumBytes;
    private boolean recordingFinishing,disposed;
    private boolean fileTask;
    private static final int PICK=401, EXPORT=402, CAMERA=403, SHARE=404, MICROPHONE=405, NOTIFICATION=406;
    Services(GofastrActivity host){this.host=host;TransferProvider.clean(host);}
    public boolean busy(){return result!=null;}
    private boolean begin(Consumer<JSONObject> callback) {if(result!=null){callback.accept(JSON.object("status","failed"));return false;}result=callback;fileTask=false;return true;}
    private void finish(JSONObject outcome) {
        if(disposed)return;
        if(fileTask){JSONObject file=outcome.optJSONObject("file");outcome=JSON.object("status",outcome.optString("status","failed"));if(file!=null)GofastrActivity.put(outcome,"file",file);}
        Consumer<JSONObject> callback=result;result=null;picked=null;permissionGranted=null;activityCode=0;
        fileTask=false;
        if(callback!=null)callback.accept(outcome);
    }
    public static JSONObject outcome(String status,String message){return JSON.object("status",status,"message",message);}
    private void failed(Exception e){Log.e("gofastr-native","device operation failed",e);releaseCapture();finish(outcome(e instanceof SecurityException?"denied":"failed",e instanceof SecurityException?"Access was denied.":"The device operation failed."));}
    public static byte[] read(InputStream input,int maximum) throws IOException {
        if(input==null)throw new IOException("No file content");
        try(InputStream in=input;ByteArrayOutputStream out=new ByteArrayOutputStream()){
            byte[] buffer=new byte[16384];int count;
            while((count=in.read(buffer))!=-1){if(out.size()+count>maximum)throw new IOException("File exceeds the transfer limit");out.write(buffer,0,count);}return out.toByteArray();
        }
    }
    public static JSONObject document(String name,String type,byte[] bytes){
        if(name==null || name.isEmpty() || name.equals(".") || name.equals("..") || name.getBytes(java.nio.charset.StandardCharsets.UTF_8).length>200 || name.contains("/") || name.contains("\\") || name.matches("(?s).*[\\p{Cntrl}].*") || type==null || type.length()>128 || !type.matches("[A-Za-z0-9!#$&^_.+-]+/[A-Za-z0-9!#$&^_.+-]+(?:;[^\\r\\n\\x00-\\x1f]*)?") || bytes.length>1<<20)throw new IllegalArgumentException("Invalid private document");
        return JSON.object("name",name,"mediaType",type,"data",Base64.encodeToString(bytes,Base64.NO_WRAP));
    }
    private void background(ThrowingSupplier operation) {
        host.workers.execute(()->{try{JSONObject value=operation.get();host.main.post(()->finish(value));}catch(Exception e){host.main.post(()->failed(e));}});
    }
    interface ThrowingSupplier {JSONObject get() throws Exception;}
    public void files(JSONObject request,Consumer<JSONObject> callback) {
        if(!begin(callback))return;
        fileTask=true;
        try {
            JSONObject extension=request.optJSONObject("extension");
            if(extension!=null){Extensions.Factory factory=Extensions.get(extension.optString("name"));if(!(factory instanceof Extensions.FileFactory)){finish(JSON.object("status","unsupported"));return;}((Extensions.FileFactory)factory).acquire(host,new JSONObject(extension.optString("payload")),this::finish);return;}
            JSONObject pick=request.optJSONObject("pick"),export=request.optJSONObject("export");
            if(pick!=null) {
                Intent intent=new Intent(Intent.ACTION_OPEN_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType("*/*");intent.putExtra(Intent.EXTRA_TITLE,pick.optString("title"));
                choose(intent,uri->background(()->{
                    String name="document",type=host.getContentResolver().getType(uri);
                    try(Cursor cursor=host.getContentResolver().query(uri,new String[]{OpenableColumns.DISPLAY_NAME},null,null,null)){if(cursor!=null && cursor.moveToFirst())name=cursor.getString(0);}
                    if(name==null || name.isEmpty())name="document";
                    JSONArray extensions=JSON.array(pick,"extensions");boolean allowed=extensions.length()==0;for(int i=0;i<extensions.length();i++)if(name.toLowerCase(Locale.ROOT).endsWith(extensions.optString(i).toLowerCase(Locale.ROOT)))allowed=true;if(!allowed)throw new IOException("This file type is not accepted");
                    byte[] bytes=read(host.getContentResolver().openInputStream(uri),Math.min(1<<20,pick.optInt("maxBytes")));
                    return JSON.object("status","completed","file",document(name,type==null?"application/octet-stream":type,bytes));
                }));
            }else if(export!=null) {
                Intent intent=new Intent(Intent.ACTION_CREATE_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType(export.optString("mediaType"));intent.putExtra(Intent.EXTRA_TITLE,export.optString("name"));
                picked=uri->background(()->{byte[] bytes=Base64.decode(export.optString("data"),Base64.DEFAULT);if(bytes.length>1<<20)throw new IOException("Export too large");try(OutputStream output=host.getContentResolver().openOutputStream(uri,"wt")){if(output==null)throw new IOException("No export destination");output.write(bytes);}return JSON.object("status","completed");});activityCode=EXPORT;host.startActivityForResult(intent,EXPORT);
            }else finish(JSON.object("status","unsupported"));
        }catch(Exception e){failed(e);}
    }
    public void choose(Intent intent,Consumer<Uri> callback) {
        picked=callback;activityCode=PICK;
        try{host.startActivityForResult(intent,PICK);}catch(Exception e){failed(e);}
    }
    public void capture(Consumer<Uri> callback) {
        try {
            if(!host.canUseCamera()){finish(JSON.object("status","unsupported"));return;}
            cameraFile=TransferProvider.create(host,"capture.jpg");Uri uri=TransferProvider.uri(host,cameraFile);
            Intent intent=new Intent(MediaStore.ACTION_IMAGE_CAPTURE).putExtra(MediaStore.EXTRA_OUTPUT,uri).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION|Intent.FLAG_GRANT_WRITE_URI_PERMISSION);
            intent.setClipData(ClipData.newRawUri("Photo",uri));picked=callback;activityCode=CAMERA;host.startActivityForResult(intent,CAMERA);
        }catch(Exception e){failed(e);}
    }
    public void releaseCapture(){if(cameraFile!=null){host.revokeUriPermission(TransferProvider.uri(host,cameraFile),Intent.FLAG_GRANT_READ_URI_PERMISSION|Intent.FLAG_GRANT_WRITE_URI_PERMISSION);TransferProvider.remove(cameraFile);cameraFile=null;}}
    public void shareText(String text,Consumer<JSONObject> callback){run(JSON.object("share",JSON.object("text",text)),callback);}
    public void run(JSONObject request,Consumer<JSONObject> callback) {
        if(!begin(callback))return;
        try {
            if(request.has("clipboard")) {
                ClipboardManager clipboard=(ClipboardManager)host.getSystemService(Context.CLIPBOARD_SERVICE);
                clipboard.setPrimaryClip(ClipData.newPlainText("",request.getJSONObject("clipboard").optString("text")));finish(outcome("completed","Copied to clipboard."));
            }else if(request.has("share")) share(request.getJSONObject("share"));
            else if(request.has("notification")) notification(request.getJSONObject("notification"));
            else if(request.has("audio")) record(request.getJSONObject("audio"));
            else finish(outcome("unsupported","This service is unavailable."));
        }catch(Exception e){failed(e);}
    }
    private void share(JSONObject request) {
        host.workers.execute(()->{
            try {
                JSONArray files=JSON.array(request,"files");if(files.length()>4)throw new IOException("Too many shared files");
                ArrayList<Uri> uris=new ArrayList<>();String type="text/plain";int total=0;
                for(int i=0;i<files.length();i++) {
                    JSONObject document=files.getJSONObject(i);byte[] bytes=Base64.decode(document.optString("data"),Base64.DEFAULT);total+=bytes.length;if(total>1<<20)throw new IOException("Share too large");
                    File file=TransferProvider.create(host,document.getString("name"));try(FileOutputStream output=new FileOutputStream(file)){output.write(bytes);}uris.add(TransferProvider.uri(host,file));
                    type=i==0?document.optString("mediaType"):"*/*";
                }
                Intent intent=new Intent(uris.size()>1?Intent.ACTION_SEND_MULTIPLE:Intent.ACTION_SEND).setType(type);
                intent.putExtra(Intent.EXTRA_TEXT,request.optString("text"));intent.putExtra(Intent.EXTRA_SUBJECT,request.optString("title"));
                if(uris.size()==1)intent.putExtra(Intent.EXTRA_STREAM,uris.get(0));else if(uris.size()>1)intent.putParcelableArrayListExtra(Intent.EXTRA_STREAM,uris);
                if(!uris.isEmpty()){ClipData clip=ClipData.newRawUri("Shared file",uris.get(0));for(int i=1;i<uris.size();i++)clip.addItem(new ClipData.Item(uris.get(i)));intent.setClipData(clip);intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);}
                host.main.post(()->{
                    try{activityCode=SHARE;host.startActivityForResult(Intent.createChooser(intent,request.optString("title","Share")),SHARE);}
                    catch(Exception e){failed(e);}
                });
            }catch(Exception e){host.main.post(()->failed(e));}
        });
    }
    private void notification(JSONObject request) throws Exception {
        String operation=request.optString("operation"),id=request.getString("identifier");
        NotificationManager manager=(NotificationManager)host.getSystemService(Context.NOTIFICATION_SERVICE);
        if(operation.equals("cancel")){ReminderReceiver.cancel(host,id);finish(JSON.object("status","completed","identifier",id,"message","Reminder cancelled."));return;}
        if(operation.equals("status")){
            String permission=manager.areNotificationsEnabled()?"authorized":"denied";
            String message=ReminderReceiver.exists(host,id)?"Reminder is scheduled.":"No reminder is scheduled.";
            finish(JSON.object("status","completed","permission",permission,"identifier",id,"message",message));return;
        }
        if(Build.VERSION.SDK_INT>=33 && host.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)!=PackageManager.PERMISSION_GRANTED){permissionGranted=()->{try{notification(request);}catch(Exception e){failed(e);}};host.requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS},NOTIFICATION);return;}
        if(!manager.areNotificationsEnabled()){finish(JSON.object("status","denied","permission","denied","identifier",id,"message","Notifications are disabled for this app."));return;}
        SimpleDateFormat format=new SimpleDateFormat("yyyy-MM-dd HH:mm",Locale.ROOT);format.setLenient(false);
        String local=request.getString("date")+" "+request.getString("time");
        Date time=format.parse(local);if(time==null || !format.format(time).equals(local))throw new IllegalArgumentException("This local time does not exist");
        long when=time.getTime();if(when<=System.currentTimeMillis())throw new IllegalArgumentException("Choose a future reminder time");
        ReminderReceiver.schedule(host,request,when);
        finish(JSON.object("status","completed","identifier",id,"permission","authorized","message","Reminder scheduled. Android may delay delivery to save battery."));
    }
    private void record(JSONObject request) {
        if(!host.canUseMicrophone()){finish(outcome("unsupported","Microphone access is not enabled for this build."));return;}
        if(host.checkSelfPermission(Manifest.permission.RECORD_AUDIO)!=PackageManager.PERMISSION_GRANTED){permissionGranted=()->record(request);host.requestPermissions(new String[]{Manifest.permission.RECORD_AUDIO},MICROPHONE);return;}
        try {
            maximumDuration=request.optInt("maxSeconds");if(maximumDuration==0)maximumDuration=60;
            maximumBytes=request.optInt("maxBytes");if(maximumBytes==0)maximumBytes=1<<20;
            if(maximumDuration<1 || maximumDuration>60 || maximumBytes<1 || maximumBytes>1<<20)throw new IOException("Invalid recording limits");
            recordingFile=File.createTempFile("recording-",".m4a",host.getCacheDir());
            recorder=new MediaRecorder();recorder.setAudioSource(MediaRecorder.AudioSource.MIC);recorder.setOutputFormat(MediaRecorder.OutputFormat.MPEG_4);recorder.setAudioEncoder(MediaRecorder.AudioEncoder.AAC);recorder.setAudioSamplingRate(44100);recorder.setAudioChannels(1);recorder.setAudioEncodingBitRate(64000);recorder.setOutputFile(recordingFile.getAbsolutePath());recorder.setMaxDuration(maximumDuration*1000);recorder.setMaxFileSize(maximumBytes);
            recorder.setOnInfoListener((r,what,extra)->{if(what==MediaRecorder.MEDIA_RECORDER_INFO_MAX_DURATION_REACHED || what==MediaRecorder.MEDIA_RECORDER_INFO_MAX_FILESIZE_REACHED)stopRecording(true);});
            recorder.setOnErrorListener((r,what,extra)->stopRecording(false));recorder.prepare();recorder.start();recordingStarted=android.os.SystemClock.elapsedRealtime();
            recordingDialog=new AlertDialog.Builder(host).setTitle(request.optString("title")).setMessage("Recording. Stop to attach this audio clip.").setNegativeButton("Cancel",(d,w)->stopRecording(false)).setPositiveButton("Stop",(d,w)->stopRecording(true)).create();
            recordingDialog.setOnCancelListener(d->stopRecording(false));recordingDialog.show();
            recordingLimit=()->stopRecording(true);host.main.postDelayed(recordingLimit,maximumDuration*1000L);
        }catch(Exception e){releaseRecorder();failed(e);}
    }
    private void stopRecording(boolean keep) {
        if(recorder==null || recordingFinishing)return;recordingFinishing=true;
        if(recordingLimit!=null)host.main.removeCallbacks(recordingLimit);
        if(recordingDialog!=null){recordingDialog.dismiss();recordingDialog=null;}
        MediaRecorder active=recorder;recorder=null;File file=recordingFile;recordingFile=null;
        int duration=(int)Math.min(maximumDuration*1000L,android.os.SystemClock.elapsedRealtime()-recordingStarted);
        host.workers.execute(()->{
            JSONObject outcome;
            try{active.stop();active.release();if(keep){byte[] data=read(new FileInputStream(file),maximumBytes);outcome=JSON.object("status","completed","durationMillis",duration,"file",document("Recording.m4a","audio/mp4",data));}else outcome=outcome("cancelled","Recording cancelled.");}
            catch(Exception e){try{active.release();}catch(Exception ignored){}outcome=outcome("failed","The recording could not be saved.");}
            finally{if(file!=null)file.delete();}
            JSONObject settled=outcome;host.main.post(()->{recordingFinishing=false;finish(settled);});
        });
    }
    private void releaseRecorder(){if(recorder!=null){try{recorder.reset();recorder.release();}catch(Exception ignored){}recorder=null;}if(recordingFile!=null){recordingFile.delete();recordingFile=null;}}
    boolean back(){if(recordingDialog!=null){stopRecording(false);return true;}return busy();}
    void pause(){if(recorder!=null)stopRecording(false);}
    void dispose(){disposed=true;releaseRecorder();if(recordingDialog!=null)recordingDialog.dismiss();if(recordingLimit!=null)host.main.removeCallbacks(recordingLimit);releaseCapture();result=null;picked=null;permissionGranted=null;}
    void activityResult(int code,int status,Intent data) {
        if(code!=activityCode || result==null)return;
        if(code==SHARE){finish(outcome(status==Activity.RESULT_OK?"completed":"cancelled","Share sheet closed. Android does not confirm delivery."));return;}
        Consumer<Uri> callback=picked;picked=null;activityCode=0;
        if(status!=Activity.RESULT_OK){releaseCapture();finish(JSON.object("status","cancelled"));return;}
        Uri uri=code==CAMERA && cameraFile!=null?Uri.fromFile(cameraFile):data==null?null:data.getData();
        if(uri==null || callback==null){releaseCapture();finish(JSON.object("status","failed"));return;}
        callback.accept(uri);
    }
    void permissionsResult(int code,int[] grants) {
        if((code!=MICROPHONE && code!=NOTIFICATION) || result==null)return;
        Runnable granted=permissionGranted;permissionGranted=null;
        if(grants.length>0 && grants[0]==PackageManager.PERMISSION_GRANTED && granted!=null)granted.run();
        else finish(outcome("denied","Permission was denied."));
    }
}
