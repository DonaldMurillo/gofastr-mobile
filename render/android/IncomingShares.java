package dev.gofastr.nativeui;

import android.app.Activity;
import android.content.ClipData;
import android.content.ContentResolver;
import android.content.Intent;
import android.database.Cursor;
import android.net.Uri;
import android.os.Handler;
import android.os.Looper;
import android.provider.OpenableColumns;
import android.util.Base64;
import org.json.JSONArray;
import org.json.JSONObject;
import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Comparator;
import java.util.LinkedHashSet;
import java.util.UUID;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;

/** Opt-in, bounded inbox. Reading never imports content or consumes an item. */
public final class IncomingShares {
    public interface Callback { void finished(JSONObject result); }
    public static final String TOKEN = "dev.gofastr.nativeui.incomingShareToken";
    private static final int MAX_BYTES=1048576, MAX_JSON=1500000, MAX_TEXT=65536, MAX_ITEMS=16;
    private static final long EXPIRY=7L*24*60*60*1000;
    private static final ThreadPoolExecutor WORKER=new ThreadPoolExecutor(1,1,0,TimeUnit.SECONDS,new ArrayBlockingQueue<Runnable>(16));
    private static final Handler MAIN=new Handler(Looper.getMainLooper());
    private IncomingShares() {}

    /** Captures cold/warm SEND intents. Preserve TOKEN through recreation until completion. */
    public static boolean capture(Activity activity, Intent intent, Callback callback) {
        if(intent==null || !(Intent.ACTION_SEND.equals(intent.getAction()) || Intent.ACTION_SEND_MULTIPLE.equals(intent.getAction()))) return false;
        String token=null;
        try { token=intent.getStringExtra(TOKEN); } catch(RuntimeException ignored) { }
        if(!identifier(token)) { token=UUID.randomUUID().toString().replace("-",""); intent.putExtra(TOKEN,token); }
        final String id=token;
        final Intent copy=new Intent(intent);
        final ContentResolver resolver=activity.getContentResolver();
        final File directory=new File(activity.getFilesDir(),"gofastr-incoming-shares");
        submit(callback,()-> {
            synchronized(IncomingShares.class) {
                prepare(directory);
                File destination=new File(directory,id+".json");
                if(!destination.exists() && !new File(directory,id+".ack").exists()) {
                    if(items(directory).length>=MAX_ITEMS) throw new IllegalArgumentException("Inbox full");
                    JSONObject item=readIntent(resolver,copy,id);
                    byte[] bytes=item.toString().getBytes(StandardCharsets.UTF_8);
                    if(bytes.length>MAX_JSON) throw new IllegalArgumentException("Transfer too large");
                    File pending=new File(directory,id+".pending");
                    try {
                        try(FileOutputStream stream=new FileOutputStream(pending)) { stream.write(bytes); stream.getFD().sync(); }
                        if(!pending.renameTo(destination)) throw new IllegalStateException("Cannot store transfer");
                    } finally { if(pending.exists()) pending.delete(); }
                }
            }
            return JSON.object("status","completed","identifier",id);
        });
        return true;
    }

    /** Handles {operation:next} or {operation:acknowledge,identifier:...}. */
    public static void perform(Activity activity, JSONObject request, Callback callback) {
        final File directory=new File(activity.getFilesDir(),"gofastr-incoming-shares");
        final JSONObject operation=JSON.copy(request);
        submit(callback,()-> {
            synchronized(IncomingShares.class) {
                prepare(directory);
                String kind=operation.optString("operation"), id=operation.optString("identifier");
                if("next".equals(kind) && id.isEmpty()) {
                    File[] entries=items(directory);
                    if(entries.length==0) return JSON.object("status","completed");
                    JSONObject item;
                    try(FileInputStream stream=new FileInputStream(entries[0])) { item=new JSONObject(new String(read(stream,MAX_JSON),StandardCharsets.UTF_8)); }
                    if(!item.optString("identifier").equals(entries[0].getName().substring(0,32))) throw new IllegalArgumentException("Invalid transfer");
                    return JSON.object("status","completed","incomingShare",item);
                }
                if("acknowledge".equals(kind) && identifier(id)) {
                    File entry=new File(directory,id+".json");
                    // A durable tombstone prevents a retained launch intent from
                    // resurrecting an item after the app has accepted it.
                    try(FileOutputStream stream=new FileOutputStream(new File(directory,id+".ack"))) { stream.getFD().sync(); }
                    if(entry.exists() && !entry.delete()) throw new IllegalStateException("Cannot acknowledge transfer");
                    prepare(directory);
                    return JSON.object("status","completed");
                }
                throw new IllegalArgumentException("Invalid operation");
            }
        });
    }

    private interface Work { JSONObject run() throws Exception; }
    private static void submit(Callback callback,Work work) {
        try { WORKER.execute(()-> { JSONObject result; try { result=work.run(); } catch(Exception error) { result=JSON.object("status","failed","message","The shared content could not be read. Use up to four files totaling 1 MiB and 64 KiB of text. The inbox holds 16 shares."); } final JSONObject reply=result; MAIN.post(()->callback.finished(reply)); }); }
        catch(java.util.concurrent.RejectedExecutionException error) { MAIN.post(()->callback.finished(JSON.object("status","failed","message","The share inbox is busy. Try again."))); }
    }

    private static JSONObject readIntent(ContentResolver resolver,Intent intent,String id) throws Exception {
        String title=intent.getStringExtra(Intent.EXTRA_SUBJECT); if(title==null) title="";
        CharSequence extra=intent.getCharSequenceExtra(Intent.EXTRA_TEXT);
        String text=extra==null?"":extra.toString();
        if(title.getBytes(StandardCharsets.UTF_8).length>200 || controls(title) || text.getBytes(StandardCharsets.UTF_8).length>MAX_TEXT || text.indexOf(0)>=0) throw new IllegalArgumentException("Invalid text");
        LinkedHashSet<Uri> uris=new LinkedHashSet<>();
        if(Intent.ACTION_SEND_MULTIPLE.equals(intent.getAction())) {
            ArrayList<Uri> streams=intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM);
            if(streams!=null) { if(streams.size()>4) throw new IllegalArgumentException("Too many files"); uris.addAll(streams); }
        } else { Uri uri=intent.getParcelableExtra(Intent.EXTRA_STREAM); if(uri!=null) uris.add(uri); }
        ClipData clip=intent.getClipData();
        if(clip!=null) { if(clip.getItemCount()>4) throw new IllegalArgumentException("Too many files"); for(int i=0;i<clip.getItemCount();i++) { Uri uri=clip.getItemAt(i).getUri(); if(uri!=null) uris.add(uri); } }
        if(uris.size()>4) throw new IllegalArgumentException("Too many files");
        JSONArray documents=new JSONArray(); int remaining=MAX_BYTES,index=0;
        for(Uri uri:uris) {
            if(!"content".equalsIgnoreCase(uri.getScheme())) throw new IllegalArgumentException("Content URI required");
            String name="shared-file-"+(++index);
            try(Cursor cursor=resolver.query(uri,new String[]{OpenableColumns.DISPLAY_NAME,OpenableColumns.SIZE},null,null,null)) {
                if(cursor!=null && cursor.moveToFirst()) {
                    int column=cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME); if(column>=0) { String proposed=cursor.getString(column); if(filename(proposed)) name=proposed; }
                    column=cursor.getColumnIndex(OpenableColumns.SIZE); if(column>=0 && !cursor.isNull(column) && cursor.getLong(column)>remaining) throw new IllegalArgumentException("Too large");
                }
            }
            byte[] bytes; try(InputStream stream=resolver.openInputStream(uri)) { if(stream==null) throw new IllegalArgumentException("No stream"); bytes=read(stream,remaining); }
            remaining-=bytes.length;
            String type=resolver.getType(uri); if(type==null || !type.matches("[A-Za-z0-9!#$&^_.+\\-]+/[A-Za-z0-9!#$&^_.+\\-]+") || type.length()>128) type="application/octet-stream";
            documents.put(JSON.object("name",name,"mediaType",type,"data",Base64.encodeToString(bytes,Base64.NO_WRAP)));
        }
        if(text.isEmpty() && documents.length()==0) throw new IllegalArgumentException("Empty share");
        JSONObject item=JSON.object("identifier",id);
        if(!title.isEmpty()) item.put("title",title);
        if(!text.isEmpty()) item.put("text",text);
        if(documents.length()>0) item.put("files",documents);
        return item;
    }

    private static byte[] read(InputStream stream,int maximum) throws Exception {
        ByteArrayOutputStream bytes=new ByteArrayOutputStream(); byte[] buffer=new byte[8192]; int count;
        while((count=stream.read(buffer,0,Math.min(buffer.length,maximum-bytes.size()+1)))!=-1) { bytes.write(buffer,0,count); if(bytes.size()>maximum) throw new IllegalArgumentException("Too large"); }
        return bytes.toByteArray();
    }
    private static boolean identifier(String id) { return id!=null && id.matches("[0-9a-f]{32}"); }
    private static boolean controls(String value) { for(int i=0;i<value.length();i++) if(Character.isISOControl(value.charAt(i))) return true; return false; }
    private static boolean filename(String name) { return name!=null && !name.isEmpty() && name.getBytes(StandardCharsets.UTF_8).length<=200 && !name.equals(".") && !name.equals("..") && !name.contains("/") && !name.contains("\\") && !controls(name); }
    private static File[] items(File directory) {
        File[] found=directory.listFiles((dir,name)->name.matches("[0-9a-f]{32}\\.json") && !new File(dir,name.substring(0,32)+".ack").exists()); if(found==null) return new File[0];
        Arrays.sort(found,Comparator.comparingLong(File::lastModified).thenComparing(File::getName)); return found;
    }
    private static void prepare(File directory) throws Exception {
        if(!directory.isDirectory() && !directory.mkdirs()) throw new IllegalStateException("Cannot create inbox");
        long oldest=System.currentTimeMillis()-EXPIRY;
        File[] found=directory.listFiles(); if(found!=null) for(File file:found) if(file.lastModified()<oldest && (file.getName().matches("[0-9a-f]{32}\\.(json|pending|ack)"))) { if(!file.delete()) throw new IllegalStateException("Cannot expire transfer"); }
        File[] acknowledgements=directory.listFiles((dir,name)->name.matches("[0-9a-f]{32}\\.ack"));
        if(acknowledgements!=null && acknowledgements.length>256) { Arrays.sort(acknowledgements,Comparator.comparingLong(File::lastModified)); for(int i=0;i<acknowledgements.length-256;i++) if(!acknowledgements[i].delete()) throw new IllegalStateException("Cannot expire acknowledgement"); }
    }
}
