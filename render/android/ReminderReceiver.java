package dev.gofastr.nativeui;

import android.app.*;
import android.content.*;
import android.net.Uri;
import android.os.Build;
import org.json.JSONObject;
import java.util.Map;

/** App-scoped local reminders. Inexact alarms need no special alarm permission. */
public final class ReminderReceiver extends BroadcastReceiver {
    private static final String CHANNEL="gofastr.reminders";
    private static SharedPreferences store(Context context){return context.getSharedPreferences("gofastr.reminders",Context.MODE_PRIVATE);}
    private static PendingIntent pending(Context context,String id,int flags) {
        Intent intent=new Intent(context,ReminderReceiver.class).setAction("gofastr.reminder").setData(new Uri.Builder().scheme("gofastr-reminder").authority(context.getPackageName()).appendPath(id).build());
        return PendingIntent.getBroadcast(context,0,intent,flags|PendingIntent.FLAG_IMMUTABLE);
    }
    static boolean exists(Context context,String id){return store(context).contains(id);}
    static void schedule(Context context,JSONObject request,long when) throws Exception {
        JSONObject stored=JSON.copy(request);stored.put("when",when);
        if(!store(context).edit().putString(request.getString("identifier"),stored.toString()).commit())throw new IllegalStateException("Cannot store reminder");
        try{arm(context,request.getString("identifier"),when);}catch(Exception e){store(context).edit().remove(request.getString("identifier")).commit();throw e;}
    }
    private static void arm(Context context,String id,long when){((AlarmManager)context.getSystemService(Context.ALARM_SERVICE)).setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP,when,pending(context,id,PendingIntent.FLAG_UPDATE_CURRENT));}
    static void cancel(Context context,String id){PendingIntent pending=pending(context,id,PendingIntent.FLAG_NO_CREATE);if(pending!=null){((AlarmManager)context.getSystemService(Context.ALARM_SERVICE)).cancel(pending);pending.cancel();}store(context).edit().remove(id).commit();((NotificationManager)context.getSystemService(Context.NOTIFICATION_SERVICE)).cancel(id,1);}
    @Override public void onReceive(Context context,Intent intent) {
        if(Intent.ACTION_BOOT_COMPLETED.equals(intent.getAction()) || Intent.ACTION_MY_PACKAGE_REPLACED.equals(intent.getAction())) {
            for(Map.Entry<String,?> entry:store(context).getAll().entrySet())try{JSONObject saved=new JSONObject((String)entry.getValue());long when=saved.getLong("when");if(when>System.currentTimeMillis())arm(context,entry.getKey(),when);else store(context).edit().remove(entry.getKey()).apply();}catch(Exception ignored){}
            return;
        }
        if(intent.getData()==null)return;String id=intent.getData().getLastPathSegment();String value=store(context).getString(id,"");if(value.isEmpty())return;
        try {
            JSONObject request=new JSONObject(value);NotificationManager manager=(NotificationManager)context.getSystemService(Context.NOTIFICATION_SERVICE);
            if(Build.VERSION.SDK_INT>=26)manager.createNotificationChannel(new NotificationChannel(CHANNEL,"Reminders",NotificationManager.IMPORTANCE_DEFAULT));
            Intent launch=context.getPackageManager().getLaunchIntentForPackage(context.getPackageName());if(launch==null)return;
            launch.setFlags(Intent.FLAG_ACTIVITY_NEW_TASK|Intent.FLAG_ACTIVITY_SINGLE_TOP|Intent.FLAG_ACTIVITY_CLEAR_TOP).setData(new Uri.Builder().scheme("gofastr-reminder").authority(context.getPackageName()).appendPath(id).build()).putExtra("gofastr.reminder",true).putExtra("route",request.optString("route"));
            PendingIntent tap=PendingIntent.getActivity(context,0,launch,PendingIntent.FLAG_UPDATE_CURRENT|PendingIntent.FLAG_IMMUTABLE);
            Notification.Builder builder=Build.VERSION.SDK_INT>=26?new Notification.Builder(context,CHANNEL):new Notification.Builder(context);
            builder.setSmallIcon(android.R.drawable.ic_popup_reminder).setContentTitle(request.optString("title")).setContentText(request.optString("body")).setStyle(new Notification.BigTextStyle().bigText(request.optString("body"))).setAutoCancel(true).setContentIntent(tap);
            manager.notify(id,1,builder.build());store(context).edit().remove(id).commit();
        }catch(Exception e){android.util.Log.e("gofastr-native","reminder delivery failed",e);}
    }
}
