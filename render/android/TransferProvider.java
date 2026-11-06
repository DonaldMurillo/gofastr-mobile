package dev.gofastr.nativeui;

import android.content.ContentProvider;
import android.content.ContentValues;
import android.content.Context;
import android.database.Cursor;
import android.database.MatrixCursor;
import android.net.Uri;
import android.os.ParcelFileDescriptor;
import android.provider.OpenableColumns;
import android.webkit.MimeTypeMap;
import java.io.*;
import java.util.*;

/** Grants access only to bounded, private, randomly named transfer directories. */
public final class TransferProvider extends ContentProvider {
    private static File root(Context context) { return new File(context.getCacheDir(),"mobile-transfers"); }
    public static File create(Context context,String name) throws IOException {
        if(name.isEmpty() || name.length()>200 || name.contains("/") || name.contains("\\") || name.equals(".") || name.equals(".."))throw new IOException("Invalid transfer name");
        clean(context);File[] existing=root(context).listFiles();long total=0;int count=0;
        if(existing!=null)for(File directory:existing){File[] files=directory.listFiles();if(files!=null)for(File file:files){count++;total+=file.length();}}
        if(count>=64 || total>=32L<<20)throw new IOException("Private transfer storage is full");
        File directory=new File(root(context),UUID.randomUUID().toString());
        if(!directory.mkdirs())throw new IOException("Cannot create private transfer");
        return new File(directory,name);
    }
    public static Uri uri(Context context,File file) {
        return new Uri.Builder().scheme("content").authority(context.getPackageName()+".transfers").appendPath(file.getParentFile().getName()).appendPath(file.getName()).build();
    }
    static void clean(Context context) {
        File[] directories=root(context).listFiles();if(directories==null)return;
        long before=System.currentTimeMillis()-24*60*60*1000L;
        for(File directory:directories)if(directory.lastModified()<before){File[] files=directory.listFiles();if(files!=null)for(File f:files)f.delete();directory.delete();}
    }
    static void remove(File file) { if(file!=null){file.delete();file.getParentFile().delete();} }
    private File resolve(Uri uri) throws FileNotFoundException {
        List<String> parts=uri.getPathSegments();
        if(parts.size()!=2 || !parts.get(0).matches("[a-f0-9-]{36}"))throw new FileNotFoundException();
        try {
            File directory=new File(root(getContext()),parts.get(0)).getCanonicalFile();
            File file=new File(directory,parts.get(1)).getCanonicalFile();
            if(!file.getParentFile().equals(directory) || !directory.getParentFile().equals(root(getContext()).getCanonicalFile()))throw new FileNotFoundException();
            return file;
        }catch(IOException e){throw new FileNotFoundException();}
    }
    @Override public boolean onCreate(){return true;}
    @Override public String getType(Uri uri){String extension=MimeTypeMap.getFileExtensionFromUrl(uri.toString());String type=MimeTypeMap.getSingleton().getMimeTypeFromExtension(extension.toLowerCase(Locale.ROOT));return type==null?"application/octet-stream":type;}
    @Override public Cursor query(Uri uri,String[] projection,String selection,String[] args,String order) {
        try{File file=resolve(uri);String[] columns=projection==null?new String[]{OpenableColumns.DISPLAY_NAME,OpenableColumns.SIZE}:projection;MatrixCursor cursor=new MatrixCursor(columns);Object[] values=new Object[columns.length];for(int i=0;i<columns.length;i++){if(columns[i].equals(OpenableColumns.DISPLAY_NAME))values[i]=file.getName();if(columns[i].equals(OpenableColumns.SIZE))values[i]=file.length();}cursor.addRow(values);return cursor;}catch(Exception e){return null;}
    }
    @Override public ParcelFileDescriptor openFile(Uri uri,String mode) throws FileNotFoundException {
        if(!mode.equals("r") && !mode.equals("w") && !mode.equals("rw"))throw new FileNotFoundException();
        int flags=mode.equals("r")?ParcelFileDescriptor.MODE_READ_ONLY:ParcelFileDescriptor.MODE_CREATE|ParcelFileDescriptor.MODE_READ_WRITE|ParcelFileDescriptor.MODE_TRUNCATE;
        return ParcelFileDescriptor.open(resolve(uri),flags);
    }
    @Override public Uri insert(Uri uri,ContentValues values){throw new UnsupportedOperationException();}
    @Override public int update(Uri uri,ContentValues values,String where,String[] args){throw new UnsupportedOperationException();}
    @Override public int delete(Uri uri,String where,String[] args){throw new UnsupportedOperationException();}
}
