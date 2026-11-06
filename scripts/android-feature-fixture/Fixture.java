package dev.gofastr.featurefixture;

import android.app.Activity;
import android.appwidget.*;
import android.content.*;
import android.database.*;
import android.net.Uri;
import android.os.*;
import android.provider.OpenableColumns;
import android.widget.*;
import java.io.*;

/** Emulator-only sender/provider and real RemoteViews host for acceptance tests. */
public final class Fixture extends Activity {
    private AppWidgetHost host;
    private int widget;
    private String target;
    @Override public void onCreate(Bundle state) {
        super.onCreate(state);
        target=getIntent().getStringExtra("target");
        if(target==null || !target.matches("[a-zA-Z][a-zA-Z0-9_.]+")){finish();return;}
        if("widget".equals(getIntent().getStringExtra("mode"))) { widget(); return; }
        try {
            File file=new File(getCacheDir(),"fixture.txt");
            try(FileOutputStream stream=new FileOutputStream(file)){
                if(getIntent().getBooleanExtra("oversized",false)){byte[] bytes=new byte[1048577];stream.write(bytes);}
                else stream.write("Shared provider bytes\n".getBytes(java.nio.charset.StandardCharsets.UTF_8));
            }
            Uri uri=Uri.parse("content://dev.gofastr.featurefixture.files/fixture.txt");
            Intent send=new Intent(Intent.ACTION_SEND).setComponent(new ComponentName(target,target+".MainActivity")).setType("text/plain")
                .putExtra(Intent.EXTRA_STREAM,uri).putExtra(Intent.EXTRA_TEXT,"Shared with a URI grant").addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION|Intent.FLAG_ACTIVITY_NEW_TASK);
            send.setClipData(ClipData.newRawUri("fixture",uri));
            startActivity(send);
            finish();
        } catch(Exception error){TextView view=new TextView(this);view.setText("Fixture failed");setContentView(view);}
    }
    private void widget(){
        host=new AppWidgetHost(this,731);
        host.startListening();
        widget=getPreferences(0).getInt("widget",0);
        AppWidgetManager manager=AppWidgetManager.getInstance(this);
        if(widget!=0 && manager.getAppWidgetInfo(widget)!=null){display();return;}
        widget=host.allocateAppWidgetId();getPreferences(0).edit().putInt("widget",widget).commit();
        ComponentName provider=new ComponentName(target,"dev.gofastr.nativeui.HomeWidget");
        Bundle options=new Bundle();options.putInt(AppWidgetManager.OPTION_APPWIDGET_MIN_WIDTH,260);options.putInt(AppWidgetManager.OPTION_APPWIDGET_MIN_HEIGHT,160);
        if(manager.bindAppWidgetIdIfAllowed(widget,provider,options)){display();return;}
        startActivityForResult(new Intent(AppWidgetManager.ACTION_APPWIDGET_BIND).putExtra(AppWidgetManager.EXTRA_APPWIDGET_ID,widget).putExtra(AppWidgetManager.EXTRA_APPWIDGET_PROVIDER,provider).putExtra(AppWidgetManager.EXTRA_APPWIDGET_OPTIONS,options),1);
    }
    private void display(){
        AppWidgetProviderInfo info=AppWidgetManager.getInstance(this).getAppWidgetInfo(widget);
        if(info==null){finish();return;}
        AppWidgetHostView view=host.createView(this,widget,info);
        LinearLayout layout=new LinearLayout(this);layout.setOrientation(LinearLayout.VERTICAL);layout.setPadding(16,48,16,16);
        TextView title=new TextView(this);title.setText("Widget test host");layout.addView(title);
        layout.addView(view,new LinearLayout.LayoutParams(-1,420));setContentView(layout);
    }
    @Override protected void onActivityResult(int code,int result,Intent data){super.onActivityResult(code,result,data);if(code==1 && result==RESULT_OK)display();else finish();}
    @Override protected void onDestroy(){if(host!=null)host.stopListening();super.onDestroy();}

    public static final class Provider extends ContentProvider {
        @Override public boolean onCreate(){return true;}
        @Override public String getType(Uri uri){return "text/plain";}
        @Override public Cursor query(Uri uri,String[] projection,String selection,String[] args,String sort){
            File file=new File(getContext().getCacheDir(),"fixture.txt");
            MatrixCursor cursor=new MatrixCursor(new String[]{OpenableColumns.DISPLAY_NAME,OpenableColumns.SIZE});cursor.addRow(new Object[]{"fixture.txt",file.length()});return cursor;
        }
        @Override public ParcelFileDescriptor openFile(Uri uri,String mode)throws FileNotFoundException{
            if(!"/fixture.txt".equals(uri.getPath()) || !"r".equals(mode))throw new FileNotFoundException();
            return ParcelFileDescriptor.open(new File(getContext().getCacheDir(),"fixture.txt"),ParcelFileDescriptor.MODE_READ_ONLY);
        }
        @Override public Uri insert(Uri uri,ContentValues values){throw new UnsupportedOperationException();}
        @Override public int update(Uri uri,ContentValues values,String where,String[] args){throw new UnsupportedOperationException();}
        @Override public int delete(Uri uri,String where,String[] args){throw new UnsupportedOperationException();}
    }
}
