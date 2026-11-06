package dev.gofastr.nativeui;

import android.content.Intent;
import android.graphics.*;
import android.media.ExifInterface;
import android.net.Uri;
import org.json.JSONObject;
import java.io.*;
import java.util.*;
import java.util.function.Consumer;

/** Bounded private JPEG normalization owned by the compiled photo library. */
public final class GofastrPhotoExtension implements Extensions.FileFactory {
    @Override public Map<String,String> capabilities(GofastrActivity host) {
        Map<String,String> caps=new LinkedHashMap<>();caps.put("photos/v1","available");caps.put("camera/v1",host.canUseCamera()?"available":"unavailable");return caps;
    }
    @Override public void acquire(GofastrActivity host,JSONObject request,Consumer<JSONObject> result) throws Exception {
        String source=request.getString("source");int maxBytes=request.getInt("maxBytes"),maxDimension=request.getInt("maxDimension");
        if(maxBytes<1 || maxBytes>1<<20 || maxDimension<1 || maxDimension>2048)throw new IOException("Invalid photo limits");
        Consumer<Uri> selected=uri->host.workers.execute(()->{
            JSONObject outcome;
            try {
                InputStream input=uri.getScheme().equals("file")?new FileInputStream(uri.getPath()):host.getContentResolver().openInputStream(uri);
                byte[] original=Services.read(input,32<<20);
                BitmapFactory.Options bounds=new BitmapFactory.Options();bounds.inJustDecodeBounds=true;BitmapFactory.decodeByteArray(original,0,original.length,bounds);
                if(bounds.outWidth<1 || bounds.outHeight<1 || (long)bounds.outWidth*bounds.outHeight>100_000_000L)throw new IOException("Image dimensions exceed decode limits");
                BitmapFactory.Options options=new BitmapFactory.Options();int sample=1;while(Math.max(bounds.outWidth,bounds.outHeight)/sample>maxDimension*2)sample*=2;options.inSampleSize=sample;
                Bitmap bitmap=BitmapFactory.decodeByteArray(original,0,original.length,options);if(bitmap==null)throw new IOException("Unsupported photo");
                try {
                    int orientation=ExifInterface.ORIENTATION_NORMAL;
                    try{orientation=new ExifInterface(new ByteArrayInputStream(original)).getAttributeInt(ExifInterface.TAG_ORIENTATION,ExifInterface.ORIENTATION_NORMAL);}catch(IOException ignored){}
                    Matrix matrix=new Matrix();
                    switch(orientation){case 2:matrix.setScale(-1,1);break;case 3:matrix.setRotate(180);break;case 4:matrix.setScale(1,-1);break;case 5:matrix.setRotate(90);matrix.postScale(-1,1);break;case 6:matrix.setRotate(90);break;case 7:matrix.setRotate(270);matrix.postScale(-1,1);break;case 8:matrix.setRotate(270);break;}
                    if(!matrix.isIdentity()){Bitmap rotated=Bitmap.createBitmap(bitmap,0,0,bitmap.getWidth(),bitmap.getHeight(),matrix,true);if(rotated!=bitmap){bitmap.recycle();bitmap=rotated;}}
                    double factor=Math.min(1.0,(double)maxDimension/Math.max(bitmap.getWidth(),bitmap.getHeight()));
                    Bitmap scaled=Bitmap.createScaledBitmap(bitmap,Math.max(1,(int)Math.round(bitmap.getWidth()*factor)),Math.max(1,(int)Math.round(bitmap.getHeight()*factor)),true);if(scaled!=bitmap){bitmap.recycle();bitmap=scaled;}
                    // Draw into an opaque bitmap so PNG alpha has a white background.
                    Bitmap opaque=Bitmap.createBitmap(bitmap.getWidth(),bitmap.getHeight(),Bitmap.Config.ARGB_8888);Canvas canvas=new Canvas(opaque);canvas.drawColor(Color.WHITE);canvas.drawBitmap(bitmap,0,0,null);bitmap.recycle();bitmap=opaque;
                    byte[] jpeg=null;
                    for(int round=0;round<5 && jpeg==null;round++) {
                        for(int quality:new int[]{85,70,50,30}) {ByteArrayOutputStream output=new ByteArrayOutputStream();if(!bitmap.compress(Bitmap.CompressFormat.JPEG,quality,output))throw new IOException("JPEG encoding failed");if(output.size()<=maxBytes){jpeg=output.toByteArray();break;}}
                        if(jpeg==null){Bitmap smaller=Bitmap.createScaledBitmap(bitmap,Math.max(1,bitmap.getWidth()/2),Math.max(1,bitmap.getHeight()/2),true);if(smaller==bitmap)break;bitmap.recycle();bitmap=smaller;}
                    }
                    if(jpeg==null)throw new IOException("Photo exceeds byte limit");
                    outcome=JSON.object("status","completed","file",Services.document("Photo.jpg","image/jpeg",jpeg));
                }finally{bitmap.recycle();}
            }catch(Exception e){android.util.Log.e("gofastr-native","photo normalization failed",e);outcome=JSON.object("status",e instanceof SecurityException?"denied":"failed");}
            JSONObject settled=outcome;host.main.post(()->{host.deviceServices().releaseCapture();result.accept(settled);});
        });
        if(source.equals("camera"))host.deviceServices().capture(selected);
        else if(source.equals("library"))host.deviceServices().choose(new Intent(Intent.ACTION_OPEN_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType("image/*"),selected);
        else throw new IOException("Unknown photo source");
    }
}
