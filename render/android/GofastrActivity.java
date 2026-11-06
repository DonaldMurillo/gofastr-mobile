package dev.gofastr.nativeui;

import android.app.Activity;
import android.app.AlertDialog;
import android.app.DatePickerDialog;
import android.app.Dialog;
import android.app.TimePickerDialog;
import android.content.Intent;
import android.content.res.Configuration;
import android.graphics.Color;
import android.graphics.Typeface;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.text.Editable;
import android.text.InputType;
import android.text.TextWatcher;
import android.util.Log;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.view.WindowInsets;
import android.view.accessibility.AccessibilityNodeInfo;
import android.widget.*;
import org.json.JSONArray;
import org.json.JSONObject;
import java.util.*;
import java.util.concurrent.*;
import java.util.function.Consumer;

/** Native Android controls for the versioned Go screen and behavior protocol. */
public abstract class GofastrActivity extends Activity {
    public interface Backend {
        void start(String directory) throws Exception;
        void stop() throws Exception;
        String root();
        String command(String payload) throws Exception;
        String render(String route,String values) throws Exception;
    }
    protected abstract Backend createBackend();
    protected String urlScheme() { return ""; }
    protected boolean cameraEnabled() { return false; }
    protected boolean microphoneEnabled() { return false; }
    protected boolean incomingSharesEnabled() { return false; }
    protected boolean secureStorageEnabled() { return false; }
    protected boolean biometricsEnabled() { return false; }
    protected boolean widgetsEnabled() { return false; }
    private static final ExecutorService lifecycle = Executors.newSingleThreadExecutor();
    final ExecutorService workers = Executors.newFixedThreadPool(4);
    private final ExecutorService snapshots = Executors.newSingleThreadExecutor();
    private final ExecutorService watches = Executors.newSingleThreadExecutor();
    final Handler main = new Handler(Looper.getMainLooper());
    private Backend backend;
    private String connection = "", activeTab = "", pendingRoute = "";
    private final Map<String,ArrayList<Frame>> stacks = new LinkedHashMap<>();
    private LinearLayout shell, bar, tabs;
    private FrameLayout viewport;
    private Frame current;
    private boolean destroyed, started, foreground, navigationPending;
    private int watchGeneration;
    private int privacyGeneration;
    private boolean privacyLockPending;
    private String lockedDestination="";
    private AlertDialog presentation;
    private Services services;
    final Appearance appearance = new Appearance(this);

    @Override public void onCreate(Bundle state) {
        setTheme(dark()?android.R.style.Theme_Material_NoActionBar:android.R.style.Theme_Material_Light_NoActionBar);
        super.onCreate(state);
        backend = createBackend();
        if(android.os.Build.VERSION.SDK_INT>=33)getOnBackInvokedDispatcher().registerOnBackInvokedCallback(android.window.OnBackInvokedDispatcher.PRIORITY_DEFAULT,()->onBackPressed());
        services = new Services(this);
        if (incomingSharesEnabled()) captureIncomingShare(getIntent(), state);
        shell = column(); shell.setBackgroundColor(background());
        shell.setOnApplyWindowInsetsListener((v, insets) -> {
            int bottom = insets.getSystemWindowInsetBottom();
            shell.setPadding(insets.getSystemWindowInsetLeft(), insets.getSystemWindowInsetTop(), insets.getSystemWindowInsetRight(), bottom);
            return insets;
        });
        bar = row(); shell.addView(bar);
        viewport = new FrameLayout(this); shell.addView(viewport, new LinearLayout.LayoutParams(-1, 0, 1));
        tabs = row(); shell.addView(tabs); setContentView(shell); systemBars();
        viewport.addView(text("Opening app...", 18));
        pendingRoute = routeFrom(getIntent());
        if (state != null && pendingRoute.isEmpty()) pendingRoute = state.getString("route", "");
        lifecycle.execute(() -> {
            try {
                backend.start(getFilesDir().getAbsolutePath()+"/app");
                Map<String,String> caps = new LinkedHashMap<>();
                for(String name: new String[]{"navigation", "editor", "drafts", "share", "haptics", "sheets", "announcement", "presentation", "files", "sharing", "clipboard", "live", "notifications", "privacy"}) caps.put(name+"/v1", "available");
                caps.put("audio/v1", microphoneEnabled() ? "available" : "unavailable");
                caps.put("incoming-shares/v1", incomingSharesEnabled() ? "available" : "unavailable");
                caps.put("secure-storage/v1", secureStorageEnabled() ? "available" : "unavailable");
                caps.put("widgets/v1", widgetsEnabled() ? "available" : "unavailable");
                caps.putAll(Extensions.capabilities(this));
                JSONObject reply = command(JSON.object("kind","hello", "hello", JSON.object("minimum",1,"maximum",1,"renderer","android","capabilities",new JSONObject(caps))));
                if (reply.has("error")) throw new IllegalStateException(reply.optJSONObject("error").optString("message"));
                connection = reply.getString("connection");
                main.post(() -> { if (!destroyed) { started = true; open(pendingRoute.isEmpty()?backend.root():pendingRoute, new LinkedHashMap<>(), "root", null); } });
            } catch (Exception e) { main.post(() -> failure("The app could not start.",e)); }
        });
    }

    @Override protected void onResume() { super.onResume(); foreground=true; reopenLocked();if(current!=null){Frame frame=current;frame.recovered=false;if(frame.deferredResponse!=null){JSONObject response=frame.deferredResponse,reply=frame.deferredReply;frame.deferredResponse=null;frame.deferredReply=null;apply(frame,response,reply);}if(!frame.busy)watch(frame);} }
    @Override public void onConfigurationChanged(Configuration configuration){super.onConfigurationChanged(configuration);setTheme(dark()?android.R.style.Theme_Material_NoActionBar:android.R.style.Theme_Material_Light_NoActionBar);shell.setBackgroundColor(background());appearance.refresh();systemBars();for(ArrayList<Frame> stack:stacks.values())for(Frame frame:stack)frame.update();toolbar();}
    private void systemBars() {
        getWindow().setStatusBarColor(background());
        getWindow().setNavigationBarColor(background());
        int flags = dark() ? 0 : View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR;
        if(android.os.Build.VERSION.SDK_INT >= 26 && !dark()) flags |= View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR;
        getWindow().getDecorView().setSystemUiVisibility(flags);
    }
    @Override protected void onPause() {
        foreground=false; stopWatch(); if(current!=null)current.suspend(); checkpoint(current, null); services.pause();
        lockProtectedFrames();
        super.onPause();
    }
    @Override protected void onSaveInstanceState(Bundle state) {
        if(getIntent()!=null && getIntent().hasExtra(IncomingShares.TOKEN)) state.putString(IncomingShares.TOKEN, getIntent().getStringExtra(IncomingShares.TOKEN));
        if(current!=null) { JSONObject protection=current.screen.optJSONObject("protection");state.putString("route",protection==null?current.screen.optString("route"):protection.optString("lockRoute")); }
        checkpoint(current, null); super.onSaveInstanceState(state);
    }

    private void lockProtectedFrames() {
        boolean found=false;
        for(ArrayList<Frame> frames:stacks.values()) {
            Iterator<Frame> iterator=frames.iterator();
            while(iterator.hasNext()) {
                Frame frame=iterator.next();
                JSONObject protection=frame.screen.optJSONObject("protection");
                if(protection==null)continue;
                found=true;
                if(frame==current)lockedDestination=protection.optString("lockRoute");
                frame.retired=true;frame.updating=true;frame.dispose();
                for(View control:frame.controls.values())if(control instanceof EditText)((EditText)control).setText("");
                frame.controls.clear();frame.values.clear();frame.editor=null;
                frame.deferredResponse=null;frame.deferredReply=null;
                frame.screen=JSON.object("route",protection.optString("lockRoute"),"title","Locked","text","Protected note locked.");
                frame.content.removeAllViews();iterator.remove();
            }
        }
        if(!found)return;
        privacyGeneration++;navigationPending=false;privacyLockPending=true;
        if(current!=null && current.retired) {
            ArrayList<Frame> frames=stack();
            current=null;viewport.removeAllViews();
            if(!frames.isEmpty())show(frames.get(frames.size()-1));
        }
        JSONObject request=JSON.object("kind","lock","connection",connection);
        shortCall(request,reply->{privacyLockPending=false;if(error(reply,current))return;reopenLocked();});
    }

    private void reopenLocked() {
        if(!foreground || privacyLockPending || lockedDestination.isEmpty() || !started)return;
        String route=lockedDestination;lockedDestination="";
        if(current==null || !current.screen.optString("route").equals(route))open(route,new LinkedHashMap<>(),"push",null);
        else refresh(current);
    }
    @Override protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent); setIntent(intent); String route=routeFrom(intent);
        if (incomingSharesEnabled()) captureIncomingShare(intent, null);
        if(!route.isEmpty()) { if(started) navigate(route); else pendingRoute=route; }
    }
    private void captureIncomingShare(Intent intent, Bundle state) {
        if(intent==null)return;
        if(state!=null && state.containsKey(IncomingShares.TOKEN))intent.putExtra(IncomingShares.TOKEN,state.getString(IncomingShares.TOKEN));
        IncomingShares.capture(this,intent,outcome -> {
            if(destroyed)return;
            if("completed".equals(outcome.optString("status"))){
                if(getIntent()==intent){intent.setAction(Intent.ACTION_MAIN);intent.removeExtra(Intent.EXTRA_STREAM);intent.removeExtra(Intent.EXTRA_TEXT);intent.setClipData(null);}
                if(current!=null)current.status("Shared content received.");
            }else if(current!=null)current.status("The shared content could not be received.");
        });
    }
    private String routeFrom(Intent intent) {
        if(intent==null) return "";
        if(intent.getBooleanExtra("gofastr.reminder", false)) return localRoute(intent.getStringExtra("route"));
        if(!Intent.ACTION_VIEW.equals(intent.getAction()))return "";
        android.net.Uri uri=intent.getData();
        if(uri==null || urlScheme().isEmpty() || !urlScheme().equals(uri.getScheme()) || uri.getFragment()!=null) return "";
        String route=(uri.getHost()==null?"":"/"+uri.getHost())+(uri.getEncodedPath()==null?"":uri.getEncodedPath());
        if(uri.getEncodedQuery()!=null) route+="?"+uri.getEncodedQuery();
        return localRoute(route);
    }
    private String localRoute(String route) {
        if(route==null || !route.startsWith("/") || route.startsWith("//") || route.contains("\\") || route.contains("#") || route.length()>4096) return "";
        return route;
    }
    @Override protected void onDestroy() {
        destroyed=true; stopWatch();
        if(presentation!=null) presentation.dismiss(); services.dispose();
        for(ArrayList<Frame> stack:stacks.values()) for(Frame f:stack) f.dispose();
        // Go Stop drains in-flight commands. Start/Stop of successive activities
        // are ordered even when Android recreates the Activity.
        snapshots.shutdown(); workers.shutdown(); watches.shutdown();
        lifecycle.execute(() -> {
            try { snapshots.awaitTermination(15,TimeUnit.SECONDS); workers.awaitTermination(15,TimeUnit.SECONDS); backend.stop(); }
            catch(Exception e) { Log.e("gofastr-native","stop failed",e); }
        });
        super.onDestroy();
    }

    JSONObject command(JSONObject request) throws Exception {
        request.put("version",1);
        JSONObject reply=new JSONObject(backend.command(request.toString()));
        if(reply.optInt("version")!=1) throw new IllegalStateException("Incompatible mobile protocol");
        return reply;
    }
    private JSONObject request(Frame f,String kind) {
        return JSON.object("kind",kind,"connection",connection,"instance",f.instance,"revision",f.revision,"values",new JSONObject(f.values));
    }
    private void shortCall(JSONObject request, Consumer<JSONObject> done) {
        snapshots.execute(() -> {
            JSONObject reply;
            try { reply=command(request); }
            catch(Exception e) { Log.e("gofastr-native","command failed",e); reply=JSON.object("error",JSON.object("code","transport","message","The app could not confirm this operation. It was not retried.")); }
            final JSONObject result=reply; main.post(() -> { if(!destroyed) done.accept(result); });
        });
    }
    private void open(String route,Map<String,String> values,String kind,Frame reuse) {
        if(navigationPending || destroyed) return;
        navigationPending=true; stopWatch();
        int generation=privacyGeneration;
        JSONObject request=JSON.object("kind","open","connection",connection,"route",route,"revision",0,"values",new JSONObject(values));
        shortCall(request, reply -> {
            if(generation!=privacyGeneration){if(reply.has("instance"))retire(reply.optString("instance"));return;}
            navigationPending=false;
            if(error(reply, current)) { if(current!=null) watch(current); return; }
            try {
                Frame f=reuse==null?new Frame():reuse;
                String previous=f.instance;
                f.instance=reply.getString("instance"); f.revision=reply.optLong("revision");
                f.screen=reply.getJSONObject("screen"); f.values=JSON.values(f.screen.optJSONObject("values"));
                f.editor=reply.optJSONObject("editor");
                if(f.editor!=null) f.values.putAll(JSON.values(f.editor.optJSONObject("values")));
                f.dataVersion=reply.optString("dataVersion");
                if(!previous.isEmpty()) retire(previous);
                if(reuse==null) {
                    f.modal=kind.equals("sheet");
                    f.build();
                    String tab=f.screen.optString("activeTab", activeTab);
                    if(tab.isEmpty()) tab="root";
                    if(stacks.isEmpty() || kind.equals("root")) activeTab=tab;
                    ArrayList<Frame> stack=stacks.computeIfAbsent(activeTab,k->new ArrayList<>());
                    if(kind.equals("replace") && !stack.isEmpty()) close(stack.remove(stack.size()-1));
                    if(kind.equals("root")) { for(Frame old:stack) close(old); stack.clear(); }
                    stack.add(f);
                } else f.update();
                show(f);
            } catch(Exception e) { failure("The screen could not open.",e); }
        });
    }
    private void retire(String instance) {
        shortCall(JSON.object("kind","close","connection",connection,"instance",instance,"revision",0), r -> {});
    }
    private void close(Frame frame) { frame.dispose(); if(!frame.instance.isEmpty()) retire(frame.instance); }
    private void show(Frame frame) {
        if(frame.retired)return;
        if(frame.screen.has("protection"))getWindow().addFlags(android.view.WindowManager.LayoutParams.FLAG_SECURE);
        else getWindow().clearFlags(android.view.WindowManager.LayoutParams.FLAG_SECURE);
        stopWatch();
        if(current!=null && current!=frame) { current.scrollY=current.scroll.getScrollY();current.rememberFocus();current.suspend();((android.view.inputmethod.InputMethodManager)getSystemService(INPUT_METHOD_SERVICE)).hideSoftInputFromWindow(current.scroll.getWindowToken(),0); }
        current=frame;
        for(ArrayList<Frame> stack:stacks.values())for(Frame retained:stack)if(retained!=frame && retained.routeDialog!=null)retained.routeDialog.hide();
        if(frame.modal){
            if(frame.routeDialog==null){
                Dialog dialog=new Dialog(this){@Override public void onBackPressed(){GofastrActivity.this.back();}};
                dialog.requestWindowFeature(android.view.Window.FEATURE_NO_TITLE);dialog.setCancelable(false);dialog.setCanceledOnTouchOutside(false);
                LinearLayout body=column();body.setBackgroundColor(background());frame.modalBar=row();body.addView(frame.modalBar);
                if(frame.scroll.getParent()!=null)((ViewGroup)frame.scroll.getParent()).removeView(frame.scroll);
                body.addView(frame.scroll,new LinearLayout.LayoutParams(-1,0,1));dialog.setContentView(body);frame.routeDialog=dialog;
                if(android.os.Build.VERSION.SDK_INT>=33)dialog.getOnBackInvokedDispatcher().registerOnBackInvokedCallback(android.window.OnBackInvokedDispatcher.PRIORITY_DEFAULT,()->back());
            }
            frame.routeDialog.show();if(frame.routeDialog.getWindow()!=null){frame.routeDialog.getWindow().setGravity(Gravity.BOTTOM);frame.routeDialog.getWindow().setLayout(-1,(int)(getResources().getDisplayMetrics().heightPixels*.86));}
            toolbar();frame.scroll.post(()->{frame.scroll.scrollTo(0,frame.scrollY);frame.restoreFocus();});return;
        }
        viewport.removeAllViews();
        if(frame.scroll.getParent()!=null) ((ViewGroup)frame.scroll.getParent()).removeView(frame.scroll);
        viewport.addView(frame.scroll); frame.scroll.post(() -> {frame.scroll.scrollTo(0,frame.scrollY);frame.restoreFocus();});
        toolbar(); if(foreground && !frame.busy) watch(frame);
    }
    private void toolbar() {
        if(current==null) return;
        Frame f=current;LinearLayout bar=f.modal && f.modalBar!=null?f.modalBar:this.bar;bar.removeAllViews();if(!f.modal)tabs.removeAllViews();
        bar.setPadding(dp(8),dp(6),dp(12),dp(6));bar.setMinimumHeight(dp(64));bar.setBackgroundColor(background());
        if(stack().size()>1) bar.addView(iconButton("Back","back","native.back",v->back()));
        JSONArray actions=JSON.array(f.screen,"actions");
        for(int i=0;i<actions.length();i++) { JSONObject a=actions.optJSONObject(i); if(a.optBoolean("leading")) bar.addView(actionButton(f,a,false)); }
        TextView title=text(f.screen.optString("title"),18); title.setTypeface(null,Typeface.BOLD); title.setSingleLine(true);title.setEllipsize(android.text.TextUtils.TruncateAt.END);
        if(f.screen.optBoolean("largeTitle")) {
            title.setAlpha(f.scroll.getScrollY()>dp(70)?1:0);
            title.setImportantForAccessibility(View.IMPORTANT_FOR_ACCESSIBILITY_NO);
            f.scroll.setOnScrollChangeListener((v,x,y,oldX,oldY)->title.setAlpha(y>dp(70)?1:0));
        }
        bar.addView(title,new LinearLayout.LayoutParams(0,-2,1));
        ArrayList<JSONObject> more=new ArrayList<>();
        boolean primary=false;String primaryID="";
        for(int i=0;i<actions.length();i++){JSONObject a=actions.optJSONObject(i);if(!a.optBoolean("leading") && (primaryID.isEmpty() || a.optBoolean("primary"))){primaryID=a.optString("id");if(a.optBoolean("primary"))break;}}
        for(int i=0;i<actions.length();i++) {
            JSONObject a=actions.optJSONObject(i); if(a.optBoolean("leading")) continue;
            if(a.optString("id").equals(primaryID) && !primary) { bar.addView(actionButton(f,a,true)); primary=true; } else more.add(a);
        }
        if(!more.isEmpty()) {
            Button overflow=iconButton("More actions","more","native.more",v->{
                PopupMenu menu=new PopupMenu(this,v);
                for(int i=0;i<more.size();i++) { JSONObject a=more.get(i); menu.getMenu().add(0,i,0,a.optString("label")).setEnabled(!f.busy); }
                menu.setOnMenuItemClickListener(item->{ action(f,more.get(item.getItemId()).optString("id"),null); return true; }); menu.show();
            }); overflow.setContentDescription("More actions");overflow.setEnabled(!f.busy && !f.uncertain); bar.addView(overflow);
        }
        JSONArray models=JSON.array(f.screen,"tabs");
        if(f.modal)return;
        tabs.setVisibility(models.length()==0?View.GONE:View.VISIBLE);
        tabs.setPadding(dp(12),dp(8),dp(12),dp(8));tabs.setBackgroundColor(appearance.surface());
        for(int i=0;i<models.length();i++) {
            JSONObject tab=models.optJSONObject(i);
            Button b=button(tab.optString("label"),"native.tab."+tab.optString("id"),v->switchTab(tab));
            b.setSelected(tab.optString("id").equals(activeTab)); b.setEnabled(!f.busy);
            int color=b.isSelected()?accent():muted();b.setTextColor(color);b.setTextSize(12);b.setMinHeight(dp(64));b.setGravity(Gravity.CENTER);
            b.setCompoundDrawables(null,appearance.icon(tab.optString("icon"),color,23),null,null);b.setCompoundDrawablePadding(dp(5));
            b.setBackground(appearance.ripple(b.isSelected()?(dark()?0xff433721:0xfff5e7cc):appearance.surface(),20,0));
            tabs.addView(b,new LinearLayout.LayoutParams(0,-2,1));
        }
    }
    private Button actionButton(Frame f,JSONObject action,boolean primary) {
        String icon=action.optString("icon"),label=action.optString("label");
        Button b=icon.equals("info")?iconButton(label,icon,"native.action."+action.optString("id"),v->action(f,action.optString("id"),null)):button(label,"native.action."+action.optString("id"),v->action(f,action.optString("id"),null));
        if(primary) {
            appearance.apply(b,Appearance.Tone.PRIMARY);b.setPadding(dp(18),dp(8),dp(18),dp(8));
            if(icon.equals("compose")){b.setCompoundDrawables(appearance.icon(icon,appearance.onAccent(),20),null,null,null);b.setCompoundDrawablePadding(dp(8));}
        }
        b.setContentDescription(label);b.setEnabled(!f.busy && !f.uncertain);return b;
    }
    private ArrayList<Frame> stack() { return stacks.computeIfAbsent(activeTab,k->new ArrayList<>()); }
    public void navigate(String route) {
        if(localRoute(route).isEmpty()) return;
        guard(() -> {
            for(Map.Entry<String,ArrayList<Frame>> entry:stacks.entrySet()) {
                ArrayList<Frame> frames=entry.getValue();
                for(int index=0;index<frames.size();index++) {
                    Frame target=frames.get(index);
                    if(target.retired || !target.screen.optString("route").equals(route))continue;
                    for(int above=index+1;above<frames.size();above++) {
                        Frame pending=frames.get(above);
                        if(pending.busy || pending.uncertain || pending.settling){current.status("Finish the current action before leaving.");return;}
                    }
                    while(frames.size()>index+1)close(frames.remove(frames.size()-1));
                    activeTab=entry.getKey();show(target);refresh(target);return;
                }
            }
            open(route,new LinkedHashMap<>(),"push",null);
        },true);
    }
    private void switchTab(JSONObject tab) {
        if(tab.optString("id").equals(activeTab)) return;
        guard(() -> {
            activeTab=tab.optString("id"); ArrayList<Frame> stack=stack();
            if(stack.isEmpty()) open(tab.optString("route"),new LinkedHashMap<>(),"push",null);
            else { stack.get(stack.size()-1).recovered=false;show(stack.get(stack.size()-1)); refresh(current); }
        });
    }
    @Override public void onBackPressed() { if(services.back()) return; back(); }
    private void back() {
        guard(() -> {
            ArrayList<Frame> stack=stack();
            if(stack.size()<=1) { finish(); return; }
            close(stack.remove(stack.size()-1)); show(stack.get(stack.size()-1)); refresh(current);
        });
    }
    private void guard(Runnable continuation) {
        guard(continuation,false);
    }
    private void guard(Runnable continuation,boolean forward) {
        Frame f=current;
        if(f==null) { continuation.run(); return; }
        if(f.busy || f.uncertain || presentation!=null || services.busy()) { f.status("Finish the current action before leaving."); return; }
        long revision=f.revision;
        checkpoint(f, reply -> {
            if(f!=current || destroyed)return;
            if(revision!=f.revision){guard(continuation,forward);return;}
            if(error(reply,f)) return;
            JSONObject editor=reply.optJSONObject("editor");
            if(editor==null || editor.optBoolean("canLeave")) { continuation.run(); return; }
            if(forward && editor.optBoolean("draftStored") && !editor.optBoolean("pending") && !editor.optBoolean("unknown")){continuation.run();return;}
            if(editor.optBoolean("pending") || editor.optBoolean("unknown")) { f.status("The save outcome is not confirmed. Check the saved notes before leaving."); return; }
            JSONObject draft=f.screen.optJSONObject("form").optJSONObject("draft");
            if(draft==null) {
                if(f.screen.has("protection"))new AlertDialog.Builder(this).setTitle("Discard unsaved protected text?")
                    .setMessage("Protected drafts stay in memory and are cleared when the app goes to the background.")
                    .setNegativeButton("Keep editing",(d,w)->{}).setPositiveButton("Discard changes",(d,w)->leave(f,"discard",continuation)).show();
                else f.status("Save your changes before leaving.");
                return;
            }
            new AlertDialog.Builder(this).setTitle(draft.optString("title")).setMessage(draft.optString("message"))
                .setNegativeButton(draft.optString("keepEditing"),(d,w)->{})
                .setNeutralButton(draft.optString("discard"),(d,w)->leave(f,"discard",continuation))
                .setPositiveButton(draft.optString("keepDraft"),(d,w)->leave(f,"keep",continuation)).show();
        });
    }
    private void leave(Frame f,String choice,Runnable continuation) {
        long revision=f.revision;
        f.settling=true;
        JSONObject request=request(f,"leave"); put(request,"choice",choice);
        shortCall(request,reply->{f.settling=false;if(error(reply,f))return; f.adoptEditor(reply,revision); if(f.revision!=revision){f.status("Your text changed. Choose Back again to review the draft.");checkpoint(f,null);return;}if(reply.optJSONObject("editor")==null || reply.optJSONObject("editor").optBoolean("canLeave")) continuation.run(); });
    }
    private void checkpoint(Frame f,Consumer<JSONObject> done) {
        if(f==null || f.retired || f.instance.isEmpty() || destroyed) return;
        if(f.settling) { f.checkpointDeferred=true; if(done!=null) f.status("Wait for the current action to finish."); return; }
        main.removeCallbacks(f.checkpointTask);
        long revision=f.revision; JSONObject request=request(f,"checkpoint");
        shortCall(request,reply->{ f.adoptEditor(reply,revision); if(done!=null) done.accept(reply); });
    }
    private void refresh(Frame f) {
        if(f==null || f.busy || f!=current || !foreground) return;
        if(f.screen.has("form")) {
            long revision=f.revision;String instance=f.instance;String route=f.screen.optString("route"),values=new JSONObject(f.values).toString();
            workers.execute(()->{try{JSONObject response=new JSONObject(backend.render(route,values));main.post(()->{if(!destroyed && f==current && !f.busy && revision==f.revision && instance.equals(f.instance) && response.optJSONObject("screen")!=null){f.screen=response.optJSONObject("screen");f.update();toolbar();}});}catch(Exception e){main.post(()->{if(f==current)f.status("The attachment list could not refresh. Your text is still here.");});}});
            return;
        }
        open(f.screen.optString("route"),new LinkedHashMap<>(f.values),"refresh",f);
    }
    private void action(Frame f,String action,Map<String,String> extra) {
        if(f!=current || f.busy || f.uncertain || navigationPending || presentation!=null || services.busy())return;
        JSONObject extras=JSON.object("action",action);
        mutate(f,"action",extras,extra,null,null);
    }
    private void mutate(Frame f,String kind,JSONObject extras,Map<String,String> extra,Runnable accepted,Runnable rejected) {
        if(f.busy || f.retired || destroyed)return;
        f.busy=true; stopWatch(); toolbar(); main.removeCallbacks(f.checkpointTask);
        String id=UUID.randomUUID().toString(); JSONObject req=request(f,kind); put(req,"requestId",id);
        if(extra!=null) { JSONObject values=req.optJSONObject("values"); for(Map.Entry<String,String> e:extra.entrySet())put(values,e.getKey(),e.getValue()); }
        Iterator<String> keys=extras.keys(); while(keys.hasNext()) {String key=keys.next(); put(req,key,extras.opt(key));}
        workers.execute(() -> {
            JSONObject reply;
            try { reply=command(req); } catch(Exception e) { reply=JSON.object("error",JSON.object("code","unknown","message","The operation outcome is unknown. It was not retried.")); }
            final JSONObject result=reply;
            main.post(() -> {
                if(destroyed || f.retired)return;
                if(!result.has("response")) {
                    JSONObject unknown=result.optJSONObject("error");
                    if(unknown!=null && unknown.optString("code").equals("validation") && (kind.equals("service-result") || kind.equals("file-result"))){
                        String key=kind.equals("service-result")?"serviceResult":"fileResult";JSONObject value=extras.optJSONObject(key);
                        if(value!=null && value.optString("status").equals("completed")){f.busy=false;JSONObject failure=JSON.copy(extras);put(failure,key,JSON.object("status","failed"));mutate(f,kind,failure,null,null,null);return;}
                    }
                    if(unknown!=null && unknown.optString("code").equals("unknown")){
                        f.settling=true;
                        JSONObject recovery=request(f,"complete");put(recovery,"requestId",id);long recoveredRevision=f.revision;
                        shortCall(recovery,settled->{f.busy=false;f.settling=false;f.adoptEditor(settled,recoveredRevision);if(settled.has("error") || !settled.has("response")){f.uncertain=true;f.status("The operation outcome is unknown. It was not retried. Reopen the app and inspect your saved data.");if(rejected!=null)rejected.run();}else{if(accepted!=null)accepted.run();JSONObject response=settled.optJSONObject("response");fenceNavigation(f,response,settled,recoveredRevision);apply(f,response,settled);}toolbar();});
                        return;
                    }
                    f.busy=false; f.adoptEditor(result,req.optLong("revision")); error(result,f);
                    if(result.optJSONObject("error")!=null && result.optJSONObject("error").optString("code").equals("unknown")) f.uncertain=true;
                    if(rejected!=null)rejected.run(); toolbar(); return;
                }
                if(accepted!=null)accepted.run();
                f.settling=true;
                JSONObject complete=request(f,"complete"); put(complete,"requestId",id); long revision=f.revision;
                shortCall(complete,settled->{
                    if(f.retired)return;
                    f.busy=false;f.settling=false; f.adoptEditor(settled,revision); toolbar();
                    if(error(settled,f)) { JSONObject err=settled.optJSONObject("error");if(err!=null && (err.optString("code").equals("transport") || err.optString("code").equals("unknown")))f.uncertain=true;if(rejected!=null)rejected.run();toolbar();return; }
                    JSONObject response=settled.optJSONObject("response");
                    fenceNavigation(f,response,settled,revision);
                    if(response!=null) apply(f,response,settled);
                    if(f.checkpointDeferred) { f.checkpointDeferred=false; checkpoint(f,null); }
                    if(f==current && !f.busy && presentation==null && !services.busy())watch(f);
                });
            });
        });
    }
    private void fenceNavigation(Frame f,JSONObject response,JSONObject reply,long sentRevision){
        if(f.revision==sentRevision || !f.screen.has("form"))return;
        if(response!=null && response.has("saved") && response.has("announcement"))put(response,"announcement","Your latest changes are still a draft.");
        if(response!=null && response.has("navigation")){response.remove("navigation");Map<String,String> ids=JSON.values(reply.optJSONObject("effectIds"));ack(f,ids.get("navigation"),"navigation/v1","cancelled");f.status("Your latest changes are still a draft.");}
        f.checkpointDeferred=true;
    }
    private void apply(Frame f,JSONObject response,JSONObject reply) {
        if(f.retired)return;
        if(!foreground){f.deferredResponse=response;f.deferredReply=reply;return;}
        JSONObject saved=response.optJSONObject("saved");
        if(saved!=null)put(f.screen,"route",saved.optString("route"));
        JSONObject update=response.optJSONObject("screen");
        if(update!=null) { f.screen=update; if(!f.screen.has("form"))f.values=JSON.values(update.optJSONObject("values")); f.update(); toolbar(); }
        Map<String,String> effects=JSON.values(reply.optJSONObject("effectIds"));
        if(response.has("haptic")) { f.scroll.performHapticFeedback(android.os.Build.VERSION.SDK_INT>=30?android.view.HapticFeedbackConstants.CONFIRM:android.view.HapticFeedbackConstants.VIRTUAL_KEY); ack(f,effects.get("haptic"),"haptics/v1","completed"); }
        if(response.has("announcement")) { f.status(response.optString("announcement"));f.scroll.announceForAccessibility(response.optString("announcement")); ack(f,effects.get("announcement"),"announcement/v1","completed"); }
        if(response.has("share")) services.shareText(response.optString("share"),result->ack(f,effects.get("share"),"share/v1",result.optString("status")));
        JSONObject nav=response.optJSONObject("navigation");
        if(nav!=null) {
            String kind=nav.optString("kind");
            if(kind.equals("pop") || kind.equals("dismiss")) {
                ArrayList<Frame> stack=stack(); if(stack.size()>1) { close(stack.remove(stack.size()-1)); show(stack.get(stack.size()-1)); refresh(current); }
            } else if(kind.equals("refresh"))refresh(f);
            else if(kind.equals("push") || kind.equals("sheet") || kind.equals("replace") || kind.equals("root"))open(nav.optString("route"),new LinkedHashMap<>(),kind,null);
            ack(f,effects.get("navigation"),kind.equals("sheet")?"sheets/v1":"navigation/v1","completed");
        }
        if(response.has("presentation")) present(f,response.optJSONObject("presentation"),reply.optString("presentationId"));
        if(response.has("files")) services.files(response.optJSONObject("files"),result -> mutate(f,"file-result",JSON.object("fileId",reply.optString("fileId"),"fileResult",result),null,null,null));
        if(response.has("service")) services.run(response.optJSONObject("service"),result -> mutate(f,"service-result",JSON.object("serviceId",reply.optString("serviceId"),"serviceResult",result),null,null,null));
        if(nav==null && !f.screen.has("form") && !response.has("presentation") && !response.has("files") && !response.has("service"))refresh(f);
    }
    private void ack(Frame f,String id,String capability,String status) {
        if(id==null || id.isEmpty())return;
        JSONObject req=request(f,"ack"); put(req,"receipt",JSON.object("id",id,"capability",capability,"status",status)); shortCall(req,r->{});
    }
    private void present(Frame f,JSONObject model,String id) {
        LinearLayout body=column(); body.setPadding(dp(20),dp(8),dp(20),dp(12));
        if(!model.optString("message").isEmpty())body.addView(text(model.optString("message"),16));
        LinkedHashSet<String> selected=new LinkedHashSet<>(); JSONObject selection=model.optJSONObject("selection");
        JSONArray initial=selection==null?new JSONArray():JSON.array(selection,"selected"); for(int i=0;i<initial.length();i++)selected.add(initial.optString(i));
        List<CompoundButton> optionViews=new ArrayList<>();
        JSONArray options=selection==null?new JSONArray():JSON.array(selection,"options");
        for(int i=0;i<options.length();i++) {
            JSONObject option=options.optJSONObject(i); String key=option.optString("id");
            CompoundButton button=selection.optBoolean("multiple")?new CheckBox(this):new RadioButton(this);
            button.setText(option.optString("label")+(option.optString("detail").isEmpty()?"":"\n"+option.optString("detail"))); button.setTextColor(ink());
            button.setChecked(selected.contains(key)); button.setEnabled(!option.optBoolean("disabled")); identify(button,"native.presentation.option."+key);
            button.setOnClickListener(v->{
                if(!selection.optBoolean("multiple")) { selected.clear(); for(CompoundButton b:optionViews) b.setChecked(b==button); }
                if(button.isChecked())selected.add(key); else selected.remove(key);
            }); optionViews.add(button);body.addView(button);
        }
        TextView message=text("",14); body.addView(message);
        ScrollView scroller=new ScrollView(this);scroller.addView(body);
        AlertDialog dialog=new AlertDialog.Builder(this).setTitle(model.optString("title")).setView(scroller).create();
        dialog.setCancelable(model.optBoolean("dismissible")); dialog.setCanceledOnTouchOutside(model.optBoolean("dismissible"));
        Consumer<String> respond=choice->{
            if(f.busy)return;
            JSONObject extra=JSON.object("presentationId",id,"choice",choice,"selected",new JSONArray(selected));
            mutate(f,"respond",extra,null,()->{ dialog.dismiss(); if(presentation==dialog)presentation=null; },()->{ JSONObject err=f.lastError; message.setText(err==null?"The choice could not be confirmed.":err.optString("message")); });
        };
        JSONArray choices=JSON.array(model,"choices");
        for(int i=0;i<choices.length();i++) { JSONObject choice=choices.optJSONObject(i); Button b=button(choice.optString("label"),"native.presentation.choice."+choice.optString("id"),v->respond.accept(choice.optString("id"))); b.setEnabled(!choice.optBoolean("disabled")); body.addView(b); }
        dialog.setOnCancelListener(d->{ if(presentation==dialog)presentation=null; respond.accept(""); });
        presentation=dialog; dialog.show();if(model.optString("kind").equals("sheet") && dialog.getWindow()!=null){dialog.getWindow().setGravity(Gravity.BOTTOM);dialog.getWindow().setLayout(-1,-2);}identify(scroller,"native.presentation");
    }
    private void stopWatch() { watchGeneration++; }
    private void watch(Frame f) {
        if(destroyed || !foreground || f!=current || f.busy || presentation!=null || services.busy() || f.screen.has("form") || f.dataVersion.isEmpty() || f.watching)return;
        int generation=watchGeneration; String instance=f.instance; JSONObject req=request(f,"watch");
        JSONObject query=new JSONObject(),list=f.screen.optJSONObject("list");
        if(list!=null){String search=list.optString("searchKey");if(!search.isEmpty())put(query,search,f.values.getOrDefault(search,""));JSONArray filters=JSON.array(list,"filters");for(int i=0;i<filters.length();i++){JSONObject field=filters.optJSONObject(i);put(query,field.optString("key"),f.values.getOrDefault(field.optString("key"),field.optString("value")));}JSONObject paging=list.optJSONObject("paging");if(paging!=null)put(query,paging.optString("key"),f.values.getOrDefault(paging.optString("key"),""));}
        put(req,"values",query);
        put(req,"dataVersion",f.dataVersion); f.watching=true;
        watches.execute(()->{
            JSONObject reply;
            try { reply=command(req); } catch(Exception e) { reply=JSON.object("error",JSON.object("code","transport","message","Live updates stopped. Use Refresh to try again.")); }
            final JSONObject result=reply;
            main.post(()->{
                f.watching=false;
                if(destroyed || !foreground || f!=current || generation!=watchGeneration || !instance.equals(f.instance)) { if(f==current && foreground)watch(f); return; }
                JSONObject err=result.optJSONObject("error");
                if(err!=null) {
                    String code=err.optString("code");
                    if((code.equals("stale") || code.equals("expired")) && !f.recovered) { f.recovered=true; refresh(f); }
                    else if(!code.equals("cancelled"))f.status(err.optString("message"));
                    return;
                }
                if(result.optBoolean("changed")) { f.recovered=false; refresh(f); }
                else { f.dataVersion=result.optString("dataVersion",f.dataVersion); main.postDelayed(()->watch(f),100); }
            });
        });
    }
    private boolean error(JSONObject reply,Frame f) {
        JSONObject err=reply.optJSONObject("error"); if(err==null)return false;
        if(f!=null){ f.lastError=err; f.status(err.optString("message","The action failed.")); JSONObject fields=err.optJSONObject("fields");
            if(fields!=null && (!reply.has("revision") || reply.optLong("revision")==f.revision)) for(String key:f.controls.keySet()){View control=f.controls.get(key);if(control instanceof EditText)((EditText)control).setError(fields.optString(key).isEmpty()?null:fields.optString(key));if(!fields.optString(key).isEmpty() && f.detailsPanel!=null){for(android.view.ViewParent parent=control.getParent();parent!=null;parent=parent.getParent())if(parent==f.detailsPanel)f.detailsPanel.setVisibility(View.VISIBLE);}}
        }else failure(err.optString("message"),null);
        return true;
    }
    private void failure(String message,Exception e) { if(e!=null)Log.e("gofastr-native",message,e); if(destroyed)return; if(current!=null)current.status(message); else { viewport.removeAllViews(); viewport.addView(text(message,18)); } }
    static void put(JSONObject object,String key,Object value) { try { object.put(key,value); }catch(Exception e){throw new IllegalArgumentException(e);} }

    final class Frame {
        String instance="",dataVersion=""; JSONObject screen,editor,lastError,deferredResponse,deferredReply;
        Map<String,String> values=new LinkedHashMap<>(); long revision; int scrollY;
        boolean busy,settling,checkpointDeferred,uncertain,watching,recovered,updating,modal,selectionMode,retired;
        Dialog routeDialog;LinearLayout modalBar;
        ScrollView scroll; LinearLayout content,rows,media,detailsPanel; TextView status;Button detailsToggle;
        final Map<String,View> controls=new LinkedHashMap<>();
        final Map<String,ComponentSlot> components=new LinkedHashMap<>();
        MediaContent legacy;
        SwipeRow revealedRow;
        View retainedFocus;int caretStart,caretEnd;boolean retainedKeyboard;
        final Runnable checkpointTask=()->checkpoint(this,null);
        Runnable searchTask;
        void rememberFocus(){retainedFocus=scroll.findFocus();android.graphics.Rect visible=new android.graphics.Rect();scroll.getWindowVisibleDisplayFrame(visible);retainedKeyboard=scroll.getRootView().getHeight()-visible.bottom>dp(180);if(retainedFocus instanceof EditText){caretStart=((EditText)retainedFocus).getSelectionStart();caretEnd=((EditText)retainedFocus).getSelectionEnd();}}
        void restoreFocus(){if(retainedFocus==null || !retainedFocus.isEnabled())return;retainedFocus.requestFocus();if(retainedFocus instanceof EditText){EditText input=(EditText)retainedFocus;int size=input.length();input.setSelection(Math.max(0,Math.min(caretStart,size)),Math.max(0,Math.min(caretEnd,size)));if(retainedKeyboard)((android.view.inputmethod.InputMethodManager)getSystemService(INPUT_METHOD_SERVICE)).showSoftInput(input,android.view.inputmethod.InputMethodManager.SHOW_IMPLICIT);}}
        void status(String message) { if(status!=null){status.setText(message); status.setVisibility(message.isEmpty()?View.GONE:View.VISIBLE);} }
        void build() {
            scroll=new ScrollView(GofastrActivity.this); scroll.setFillViewport(true); identify(scroll,"native.screen");
            content=column();content.setPadding(dp(20),dp(12),dp(20),dp(28)); scroll.addView(content);
            if(screen.optBoolean("largeTitle")){TextView heading=text(screen.optString("title"),34);appearance.typography(heading,Appearance.Tone.HEADING);heading.setPadding(0,0,0,dp(4));if(android.os.Build.VERSION.SDK_INT>=28)heading.setAccessibilityHeading(true);content.addView(heading);}
            if(!screen.optString("prompt").isEmpty()){TextView prompt=text(screen.optString("prompt"),14);appearance.typography(prompt,Appearance.Tone.SECONDARY);prompt.setPadding(0,0,0,dp(20));content.addView(prompt);}
            status=text("",13);appearance.typography(status,Appearance.Tone.SECONDARY);identify(status,"native.status");status.setAccessibilityLiveRegion(View.ACCESSIBILITY_LIVE_REGION_POLITE);content.addView(status);status("");
            JSONObject form=screen.optJSONObject("form"),list=screen.optJSONObject("list");
            if(form!=null) {
                status(form.optString("status"));
                JSONArray fields=JSON.array(form,"fields");
                LinearLayout details=column();details.setPadding(dp(16),dp(8),dp(16),dp(16));appearance.apply(details,Appearance.Tone.SURFACE);
                for(int i=0;i<fields.length();i++){JSONObject field=fields.optJSONObject(i); addField(field,field.optString("style").equals("detail")?details:content,false);}
                if(details.getChildCount()>0) {
                    String detailsTitle=form.optString("detailsTitle","Details");
                    detailsToggle=button(detailsTitle,"native.details",v->{details.setVisibility(details.getVisibility()==View.VISIBLE?View.GONE:View.VISIBLE);disclosure(detailsToggle,detailsTitle,details.getVisibility()==View.VISIBLE);});
                    appearance.apply(detailsToggle,Appearance.Tone.OUTLINE);detailsToggle.setGravity(Gravity.START|Gravity.CENTER_VERTICAL);disclosure(detailsToggle,detailsTitle,false);detailsPanel=details;details.setVisibility(View.GONE);
                }
            }
            if(list!=null) {
                String search=list.optString("searchKey");
                if(!search.isEmpty()) {
                    EditText input=new EditText(GofastrActivity.this);input.setSingleLine(true);input.setTextSize(16);input.setHint(list.optString("searchPlaceholder","Search"));input.setText(values.getOrDefault(search,""));input.setContentDescription(list.optString("searchPlaceholder","Search"));identify(input,"native.search");
                    appearance.apply(input,Appearance.Tone.SEARCH);input.setPadding(dp(16),dp(14),dp(16),dp(14));input.setCompoundDrawables(appearance.icon("search",muted(),20),null,null,null);input.setCompoundDrawablePadding(dp(10));
                    controls.put(search,input);content.addView(input,new LinearLayout.LayoutParams(-1,-2));
                    input.addTextChangedListener(watcher(value->{ if(updating)return;values.put(search,value);revision++;scheduleRefresh(); }));
                }
                JSONArray filters=JSON.array(list,"filters");LinearLayout filterControls=column();for(int i=0;i<filters.length();i++)addField(filters.optJSONObject(i),filterControls,true);
                LinearLayout tools=row();tools.setPadding(0,dp(8),0,dp(8));
                if(filters.length()>0){Button filtersButton=button("Filters","native.filters",v->{filterControls.setVisibility(filterControls.getVisibility()==View.VISIBLE?View.GONE:View.VISIBLE);disclosure((Button)v,"Filters",filterControls.getVisibility()==View.VISIBLE);});disclosure(filtersButton,"Filters",false);tools.addView(filtersButton);}
                tools.addView(new View(GofastrActivity.this),new LinearLayout.LayoutParams(0,1,1));
                if(list.has("selection"))tools.addView(button("Select","native.select",v->{selectionMode=!selectionMode;((Button)v).setText(selectionMode?"Done":"Select");if(!selectionMode){values.put(list.optJSONObject("selection").optString("key"),"[]");revision++;}update();}));
                if(list.optBoolean("refreshable"))tools.addView(button("Refresh","native.refresh",v->{recovered=false;refresh(this);}));content.addView(tools);
                filterControls.setPadding(dp(16),dp(8),dp(16),dp(12));appearance.apply(filterControls,Appearance.Tone.SURFACE);filterControls.setVisibility(View.GONE);content.addView(filterControls);
            }
            rows=column();content.addView(rows); media=column();content.addView(media);if(detailsPanel!=null){LinearLayout.LayoutParams layout=new LinearLayout.LayoutParams(-1,-2);layout.topMargin=dp(20);content.addView(detailsToggle,layout);content.addView(detailsPanel);}update();
        }
        void scheduleRefresh() {
            if(searchTask!=null)main.removeCallbacks(searchTask);
            searchTask=()->refresh(this);main.postDelayed(searchTask,300);
        }
        void changed(String key,String value,boolean filter) {
            if(updating || value.equals(values.get(key)))return; values.put(key,value);revision++;
            if(filter)scheduleRefresh();else { main.removeCallbacks(checkpointTask);main.postDelayed(checkpointTask,350); status(screen.optJSONObject("form").optJSONObject("draft")==null?"":screen.optJSONObject("form").optJSONObject("draft").optString("changed")); }
        }
        void addField(JSONObject field,LinearLayout target,boolean filter) {
            String key=field.optString("key"),kind=field.optString("kind"),value=values.getOrDefault(key,field.optString("value"));values.put(key,value);
            String label=field.optString("label"); boolean disabled=field.optBoolean("disabled"); View control;
            if(kind.equals("toggle")) {
                Switch toggle=new Switch(GofastrActivity.this);toggle.setText(label);appearance.apply(toggle,Appearance.Tone.BODY);toggle.setTextSize(15);toggle.setMinHeight(dp(52));toggle.setChecked(value.equals("true"));toggle.setOnCheckedChangeListener((v,checked)->changed(key,Boolean.toString(checked),filter));control=toggle;target.addView(control,new LinearLayout.LayoutParams(-1,-2));
            } else if(kind.equals("select")) {
                target.addView(sectionLabel(label));Button select=button(optionLabel(field,value),"native.field."+key,v->{
                    JSONArray opts=JSON.array(field,"options"); List<String> labels=new ArrayList<>();List<String> vals=new ArrayList<>();
                    for(int i=0;i<opts.length();i++){JSONObject opt=opts.optJSONObject(i);if(!opt.optBoolean("disabled")){labels.add(opt.optString("label"));vals.add(opt.optString("value"));}}
                    new AlertDialog.Builder(GofastrActivity.this).setTitle(label).setItems(labels.toArray(new String[0]),(d,which)->{((Button)v).setText(labels.get(which));changed(key,vals.get(which),filter);}).show();
                });appearance.apply(select,Appearance.Tone.OUTLINE);select.setGravity(Gravity.START|Gravity.CENTER_VERTICAL);control=select;target.addView(control,new LinearLayout.LayoutParams(-1,-2));
            } else if(kind.equals("date") || kind.equals("time")) {
                target.addView(sectionLabel(label));Button date=button(value.isEmpty()?field.optString("placeholder","Choose"):value,"native.field."+key,v->{
                    Calendar now=Calendar.getInstance();String current=values.getOrDefault(key,"");
                    Consumer<String> set=next->{((Button)v).setText(next.isEmpty()?"Choose":next);changed(key,next,filter);};
                    if(kind.equals("date")) {
                        int y=now.get(Calendar.YEAR),m=now.get(Calendar.MONTH),d=now.get(Calendar.DAY_OF_MONTH);try{String[] parts=current.split("-");y=Integer.parseInt(parts[0]);m=Integer.parseInt(parts[1])-1;d=Integer.parseInt(parts[2]);}catch(Exception ignored){}
                        DatePickerDialog picker=new DatePickerDialog(GofastrActivity.this,(view,year,month,day)->set.accept(String.format(Locale.ROOT,"%04d-%02d-%02d",year,month+1,day)),y,m,d);picker.setButton(AlertDialog.BUTTON_NEUTRAL,"Clear",(dialog,which)->set.accept(""));picker.show();
                    }else {
                        int h=now.get(Calendar.HOUR_OF_DAY),m=now.get(Calendar.MINUTE);try{String[] parts=current.split(":");h=Integer.parseInt(parts[0]);m=Integer.parseInt(parts[1]);}catch(Exception ignored){}
                        TimePickerDialog picker=new TimePickerDialog(GofastrActivity.this,(view,hour,minute)->set.accept(String.format(Locale.ROOT,"%02d:%02d",hour,minute)),h,m,true);picker.setButton(AlertDialog.BUTTON_NEUTRAL,"Clear",(dialog,which)->set.accept(""));picker.show();
                    }
                });appearance.apply(date,Appearance.Tone.OUTLINE);date.setGravity(Gravity.START|Gravity.CENTER_VERTICAL);control=date;target.addView(control,new LinearLayout.LayoutParams(-1,-2));
            }else {
                boolean multiline=kind.equals("multiline");
                if(!multiline && !field.optString("style").equals("body") && !field.optString("style").equals("title"))target.addView(sectionLabel(label));
                EditText input=new EditText(GofastrActivity.this); input.setTextColor(ink());input.setHintTextColor(muted());input.setTextSize(field.optString("style").equals("title")?28:18);
                input.setInputType(InputType.TYPE_CLASS_TEXT|InputType.TYPE_TEXT_FLAG_CAP_SENTENCES|(multiline?InputType.TYPE_TEXT_FLAG_MULTI_LINE:0));input.setSingleLine(!multiline);input.setGravity(Gravity.TOP|Gravity.START);if(multiline)input.setMinLines(10);
                if(screen.has("protection")) {
                    input.setInputType(input.getInputType()|InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);
                    input.setSaveEnabled(false);
                    if(android.os.Build.VERSION.SDK_INT>=26) {
                        input.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO_EXCLUDE_DESCENDANTS);
                        input.setImeOptions(input.getImeOptions()|android.view.inputmethod.EditorInfo.IME_FLAG_NO_PERSONALIZED_LEARNING);
                    }
                }
                input.setHint(field.optString("placeholder",label));input.setText(value);input.setContentDescription(label);input.addTextChangedListener(watcher(next->changed(key,next,filter)));control=input;target.addView(control,new LinearLayout.LayoutParams(-1,-2));
                if(multiline || field.optString("style").equals("title")) {
                    appearance.typography(input,field.optString("style").equals("title")?Appearance.Tone.DOCUMENT_TITLE:Appearance.Tone.DOCUMENT_BODY);
                    input.setBackgroundColor(Color.TRANSPARENT);input.setPadding(0,dp(10),0,dp(14));
                }else {appearance.apply(input,Appearance.Tone.INPUT);input.setTextSize(16);input.setPadding(dp(12),dp(14),dp(12),dp(14));}
            }
            control.setEnabled(!disabled);identify(control,"native.field."+key);controls.put(key,control);
        }
        void adoptEditor(JSONObject reply,long sentRevision) {
            if(retired)return;
            JSONObject state=reply.optJSONObject("editor");if(state==null || sentRevision!=revision)return;
            editor=state;
            if(!state.optString("message").isEmpty())status(state.optString("message"));
            else if(state.optBoolean("restored")) { JSONObject draft=screen.optJSONObject("form").optJSONObject("draft"); if(draft!=null)status(draft.optString("restored")); }
            if(state.has("values")) {
                Map<String,String> latest=JSON.values(state.optJSONObject("values")); updating=true;
                for(Map.Entry<String,String> e:latest.entrySet()) {
                    if(e.getValue().equals(values.get(e.getKey())))continue; values.put(e.getKey(),e.getValue());View control=controls.get(e.getKey());
                    if(control instanceof EditText)((EditText)control).setText(e.getValue());
                    else if(control instanceof Switch)((Switch)control).setChecked(e.getValue().equals("true"));
                    else if(control instanceof Button)((Button)control).setText(e.getValue());
                }
                updating=false;
            }
        }
        void update() {
            revealedRow=null;
            rows.removeAllViews();JSONObject list=screen.optJSONObject("list");
            if(list!=null) {
                if(!list.optString("header").isEmpty()){TextView header=sectionLabel(list.optString("header"));header.setPadding(dp(4),dp(12),dp(4),dp(12));rows.addView(header);}
                JSONArray items=JSON.array(list,"rows"); JSONObject selection=list.optJSONObject("selection");LinkedHashSet<String> selected=new LinkedHashSet<>();
                if(selection!=null){JSONArray ids=JSON.array(selection,"selected");try{String saved=values.get(selection.optString("key"));if(saved!=null)ids=new JSONArray(saved);}catch(Exception ignored){}for(int i=0;i<ids.length();i++)selected.add(ids.optString(i));}
                if(items.length()==0 && !list.optString("empty").isEmpty()) {
                    LinearLayout empty=column();appearance.apply(empty,Appearance.Tone.SURFACE);empty.setGravity(Gravity.CENTER);empty.setPadding(dp(24),dp(36),dp(24),dp(36));
                    TextView symbol=text("",24);symbol.setCompoundDrawables(null,appearance.icon("notes",accent(),32),null,null);symbol.setImportantForAccessibility(View.IMPORTANT_FOR_ACCESSIBILITY_NO);empty.addView(symbol);
                    String[] message=list.optString("empty").split("\n",2);TextView heading=text(message[0],18);heading.setTypeface(null,Typeface.BOLD);heading.setGravity(Gravity.CENTER);empty.addView(heading);
                    if(message.length>1){TextView hint=text(message[1],14);appearance.apply(hint,Appearance.Tone.SECONDARY);hint.setGravity(Gravity.CENTER);empty.addView(hint);}
                    rows.addView(empty,new LinearLayout.LayoutParams(-1,-2));
                }
                for(int i=0;i<items.length();i++) {
                    JSONObject item=items.optJSONObject(i);String id=item.optString("id");LinearLayout line=row();line.setMinimumHeight(dp(80));line.setPadding(dp(12),dp(4),dp(4),dp(4));appearance.apply(line,Appearance.Tone.SURFACE);
                    if(selection!=null && (selectionMode || !selected.isEmpty())) {
                        CheckBox check=new CheckBox(GofastrActivity.this);appearance.apply(check,Appearance.Tone.BODY);check.setChecked(selected.contains(id));check.setEnabled(!item.optBoolean("disabled"));check.setContentDescription("Select "+item.optString("title"));identify(check,"native.selection."+id);
                        check.setOnCheckedChangeListener((v,checked)->{ if(checked)selected.add(id);else selected.remove(id);values.put(selection.optString("key"),new JSONArray(selected).toString());revision++; });line.addView(check);
                    }
                    if(!item.optString("icon").isEmpty()){TextView icon=text("",18);icon.setCompoundDrawables(appearance.icon(item.optString("icon"),accent(),24),null,null,null);icon.setImportantForAccessibility(View.IMPORTANT_FOR_ACCESSIBILITY_NO);icon.setPadding(0,0,dp(12),0);line.addView(icon);}
                    LinearLayout labels=column();TextView title=text(item.optString("title"),17);title.setTypeface(Typeface.create("sans-serif-medium",Typeface.NORMAL));title.setPadding(dp(4),dp(4),dp(4),dp(3));labels.addView(title);
                    if(!item.optString("subtitle").isEmpty()){TextView subtitle=text(item.optString("subtitle"),14);appearance.apply(subtitle,Appearance.Tone.SECONDARY);subtitle.setMaxLines(2);subtitle.setEllipsize(android.text.TextUtils.TruncateAt.END);subtitle.setPadding(dp(4),dp(2),dp(4),dp(2));labels.addView(subtitle);}
                    if(!item.optString("metadata").isEmpty()){TextView metadata=text(item.optString("metadata"),12);appearance.apply(metadata,Appearance.Tone.SECONDARY);metadata.setPadding(dp(4),dp(5),dp(4),dp(2));labels.addView(metadata);}
                    labels.setPadding(dp(4),dp(8),dp(4),dp(10));labels.setEnabled(!item.optBoolean("disabled"));identify(labels,"native.row."+id);labels.setOnClickListener(v->{if(!item.optBoolean("disabled"))navigate(item.optString("destination"));});line.addView(labels,new LinearLayout.LayoutParams(0,-2,1));
                    JSONArray trailing=JSON.array(item,"trailingActions"),leading=JSON.array(item,"leadingActions");
                    View rowView=line;
                    if(trailing.length()+leading.length()>0) {
                        SwipeRow swipe=new SwipeRow(GofastrActivity.this,line,id,leading,trailing,
                                ()->this==current && !busy && !uncertain && !selectionMode && !item.optBoolean("disabled"),
                                selectedAction->action(this,selectedAction.optString("id"),Collections.singletonMap("row",id)),
                                opened->{
                                    if(revealedRow!=null && revealedRow!=opened)revealedRow.close();
                                    revealedRow=opened;
                                });
                        ArrayList<JSONObject> menuActions=new ArrayList<>();
                        for(int j=0;j<leading.length();j++)menuActions.add(leading.optJSONObject(j));
                        for(int j=0;j<trailing.length();j++)menuActions.add(trailing.optJSONObject(j));
                        Button more=iconButton("Actions for "+item.optString("title"),"more",
                                "native.row.actions."+id,v->{
                                    swipe.close();
                                    PopupMenu menu=new PopupMenu(GofastrActivity.this,v);
                                    for(int j=0;j<menuActions.size();j++)
                                        menu.getMenu().add(0,j,0,menuActions.get(j).optString("label"));
                                    menu.setOnMenuItemClickListener(entry->{
                                        if(this==current && !item.optBoolean("disabled"))
                                            action(this,menuActions.get(entry.getItemId()).optString("id"),Collections.singletonMap("row",id));
                                        return true;
                                    });
                                    menu.show();
                                });
                        more.setEnabled(!item.optBoolean("disabled") && !selectionMode);
                        line.addView(more);
                        rowView=swipe;
                    } else {
                        TextView chevron=text("",16);chevron.setCompoundDrawables(appearance.icon("chevron",muted(),18),null,null,null);chevron.setPadding(dp(8),0,dp(12),0);chevron.setImportantForAccessibility(View.IMPORTANT_FOR_ACCESSIBILITY_NO);line.addView(chevron);
                    }
                    LinearLayout.LayoutParams layout=new LinearLayout.LayoutParams(-1,-2);layout.bottomMargin=dp(8);rows.addView(rowView,layout);
                }
                if(!list.optString("footer").isEmpty()){TextView footer=text(list.optString("footer"),12);appearance.apply(footer,Appearance.Tone.SECONDARY);footer.setPadding(dp(4),dp(16),dp(4),dp(8));rows.addView(footer);}
                JSONObject paging=list.optJSONObject("paging");if(paging!=null && !paging.optString("next").isEmpty())rows.addView(button(paging.optString("label"),"native.paging",v->{values.put(paging.optString("key"),paging.optString("next"));revision++;refresh(this);}));
            }
            if(!screen.optString("text").isEmpty()) {TextView body=text(screen.optString("text"),18);body.setTextIsSelectable(true);rows.addView(body);}
            reconcileComponents();
            if(editor!=null && editor.optBoolean("restored")) { JSONObject form=screen.optJSONObject("form"),draft=form==null?null:form.optJSONObject("draft");if(draft!=null)status(draft.optString("restored")); }
        }
        void reconcileComponents() {
            try {
                JSONArray images=JSON.array(screen,"images"),files=JSON.array(screen,"attachments");JSONObject preview=screen.optJSONObject("filePreview");
                if(images.length()+files.length()>0 || preview!=null){Map<String,String> routes=new LinkedHashMap<>();for(int i=0;i<images.length();i++)routes.put("image."+i,images.optJSONObject(i).optString("destination"));for(int i=0;i<files.length();i++)routes.put("file."+i,files.optJSONObject(i).optString("destination"));JSONObject payload=JSON.object("images",images,"attachments",files);if(preview!=null)put(payload,"preview",preview);Consumer<String> emit=key->{String route=routes.getOrDefault(key,"");if(!route.isEmpty() && this==current)navigate(route);};if(legacy==null){legacy=new MediaContent(GofastrActivity.this,payload,emit);media.addView(legacy.view(),0);}else legacy.update(payload,emit);}
                else if(legacy!=null){legacy.dispose();media.removeView(legacy.view());legacy=null;}
            }catch(Exception e){failure("The media preview could not be displayed.",e);}
            Set<String> retained=new HashSet<>();JSONArray models=JSON.array(screen,"components");
            for(int i=0;i<models.length();i++) {
                JSONObject model=models.optJSONObject(i);String id=model.optString("id"),name=model.optString("name");retained.add(id);
                try {
                    ComponentSlot slot=components.get(id);JSONObject routes=model.optJSONObject("destinations");
                    Consumer<String> emit=key->{String route=routes==null?"":routes.optString(key);if(!route.isEmpty() && this==current)navigate(route);};
                    if(slot==null || !slot.name.equals(name)) {
                        if(slot!=null){slot.component.dispose();media.removeView(slot.component.view());}
                        Extensions.Factory factory=Extensions.get(name);if(!(factory instanceof Extensions.ComponentFactory))throw new IllegalStateException("No compiled component adapter");
                        Extensions.Component component=((Extensions.ComponentFactory)factory).mount(GofastrActivity.this,new JSONObject(model.optString("payload")),emit);
                        slot=new ComponentSlot(name,component);components.put(id,slot);media.addView(component.view());
                    }else slot.component.update(new JSONObject(model.optString("payload")),emit);
                } catch(Exception e) { failure("The attachment preview could not be displayed.",e); }
            }
            Iterator<Map.Entry<String,ComponentSlot>> iterator=components.entrySet().iterator();
            while(iterator.hasNext()){Map.Entry<String,ComponentSlot> e=iterator.next();if(!retained.contains(e.getKey())){e.getValue().component.dispose();media.removeView(e.getValue().component.view());iterator.remove();}}
        }
        void suspend() { if(revealedRow!=null)revealedRow.close();if(legacy!=null)legacy.suspend();for(ComponentSlot s:components.values())s.component.suspend(); }
        void dispose() { main.removeCallbacks(checkpointTask);if(searchTask!=null)main.removeCallbacks(searchTask);if(routeDialog!=null){routeDialog.dismiss();routeDialog=null;}if(legacy!=null)legacy.dispose();for(ComponentSlot s:components.values())s.component.dispose();components.clear(); }
    }
    private static class ComponentSlot { final String name;final Extensions.Component component;ComponentSlot(String name,Extensions.Component component){this.name=name;this.component=component;} }
    String optionLabel(JSONObject field,String value) { JSONArray options=JSON.array(field,"options");for(int i=0;i<options.length();i++){JSONObject o=options.optJSONObject(i);if(o.optString("value").equals(value))return o.optString("label");}return value; }
    static TextWatcher watcher(Consumer<String> changed) { return new TextWatcher(){public void beforeTextChanged(CharSequence s,int start,int count,int after){} public void onTextChanged(CharSequence s,int start,int before,int count){changed.accept(s.toString());}public void afterTextChanged(Editable e){}}; }
    public int dp(int size){return Math.round(size*getResources().getDisplayMetrics().density);}
    public boolean dark(){return (getResources().getConfiguration().uiMode&Configuration.UI_MODE_NIGHT_MASK)==Configuration.UI_MODE_NIGHT_YES;}
    public int ink(){return dark()?0xffeeeae3:0xff24231f;}
    public int muted(){return dark()?0xffb8b4ac:0xff69665f;}
    public int background(){return dark()?0xff1b1a17:0xfff7f5ef;}
    public int accent(){return dark()?0xffffcc68:0xff925500;}
    public LinearLayout column(){LinearLayout v=new LinearLayout(this);v.setOrientation(LinearLayout.VERTICAL);return v;}
    public LinearLayout row(){LinearLayout v=new LinearLayout(this);v.setOrientation(LinearLayout.HORIZONTAL);v.setGravity(Gravity.CENTER_VERTICAL);return v;}
    public TextView text(String text,int size){TextView v=new TextView(this);v.setText(text);v.setTextSize(size);appearance.typography(v,Appearance.Tone.BODY);v.setPadding(dp(4),dp(6),dp(4),dp(6));return v;}
    public Button button(String label,String id,View.OnClickListener listener){Button b=new Button(this,null,android.R.attr.borderlessButtonStyle);b.setText(label);b.setAllCaps(false);b.setTextSize(14);b.setMinWidth(0);b.setMinimumWidth(0);b.setPadding(dp(12),dp(8),dp(12),dp(8));b.setMinHeight(dp(48));appearance.apply(b,Appearance.Tone.ACTION);b.setOnClickListener(listener);identify(b,id);return b;}
    Button iconButton(String label,String icon,String id,View.OnClickListener listener){Button b=button("",id,listener);b.setContentDescription(label);b.setCompoundDrawables(appearance.icon(icon,accent(),24),null,null,null);b.setPadding(dp(12),dp(12),dp(12),dp(12));return b;}
    TextView sectionLabel(String label){TextView v=text(label,13);appearance.typography(v,Appearance.Tone.SECTION);v.setPadding(dp(4),dp(16),dp(4),dp(8));return v;}
    void disclosure(Button button,String label,boolean expanded){button.setCompoundDrawables(null,null,appearance.icon(expanded?"down":"chevron",muted(),16),null);button.setCompoundDrawablePadding(dp(8));button.setContentDescription(label+", "+(expanded?"expanded":"collapsed"));}
    public static void identify(View view,String id){ view.setTag(id); view.setAccessibilityDelegate(new View.AccessibilityDelegate(){@Override public void onInitializeAccessibilityNodeInfo(View host,AccessibilityNodeInfo info){super.onInitializeAccessibilityNodeInfo(host,info);info.setViewIdResourceName(id);}}); }
    public boolean canUseCamera(){return cameraEnabled();}
    public boolean canUseMicrophone(){return microphoneEnabled();}
    public Services deviceServices(){return services;}
    @Override protected void onActivityResult(int requestCode,int resultCode,Intent data){super.onActivityResult(requestCode,resultCode,data);services.activityResult(requestCode,resultCode,data);}
    @Override public void onRequestPermissionsResult(int requestCode,String[] permissions,int[] results){super.onRequestPermissionsResult(requestCode,permissions,results);services.permissionsResult(requestCode,results);}
}
