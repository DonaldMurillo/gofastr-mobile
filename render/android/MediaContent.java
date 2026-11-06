package dev.gofastr.nativeui;

import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.media.MediaPlayer;
import android.util.Base64;
import android.view.View;
import android.widget.*;
import org.json.*;
import java.io.*;
import java.util.*;
import java.util.function.Consumer;

/** Reusable nativeui.Media views, with keyed images and local audio resources. */
public final class MediaContent implements Extensions.Component {
    private final GofastrActivity host;
    private final LinearLayout root;
    private Consumer<String> emit;
    private final Map<String,Item> items=new LinkedHashMap<>();
    private boolean disposed;
    public MediaContent(GofastrActivity host,JSONObject payload,Consumer<String> emit) throws Exception {this.host=host;root=host.column();update(payload,emit);}
    @Override public View view(){return root;}
    @Override public void update(JSONObject payload,Consumer<String> emitter) throws Exception {
        if(disposed)return;emit=emitter;LinkedHashSet<String> retained=new LinkedHashSet<>();
        JSONArray images=JSON.array(payload,"images"),files=JSON.array(payload,"attachments");
        for(int i=0;i<images.length();i++) {
            JSONObject image=images.getJSONObject(i);String key="image."+image.optString("id"),event="image."+i;retained.add(key);Item item=items.get(key);
            if(item==null){item=new Item();item.box=host.column();items.put(key,item);root.addView(item.box);}
            String data=image.optString("data");
            if(!data.equals(item.data)) {
                item.box.removeAllViews();if(item.bitmap!=null)item.bitmap.recycle();byte[] bytes=Base64.decode(data,Base64.DEFAULT);if(bytes.length>128<<10)throw new IOException("Preview too large");
                item.bitmap=BitmapFactory.decodeByteArray(bytes,0,bytes.length);if(item.bitmap==null || item.bitmap.getWidth()>1024 || item.bitmap.getHeight()>1024)throw new IOException("Invalid preview");
                ImageView view=new ImageView(host);view.setImageBitmap(item.bitmap);view.setAdjustViewBounds(true);view.setMaxHeight(host.dp(320));view.setScaleType(ImageView.ScaleType.FIT_CENTER);item.image=view;item.box.addView(view,new LinearLayout.LayoutParams(-1,-2));item.data=data;
            }
            item.image.setContentDescription(image.optString("alt"));GofastrActivity.identify(item.image,"native.image."+image.optString("id"));
            item.image.setOnClickListener(v->emit.accept(event));item.image.setClickable(!image.optString("destination").isEmpty());
            order(item.box,retained.size()-1);
        }
        for(int i=0;i<files.length();i++) {
            JSONObject file=files.getJSONObject(i);String key="file."+file.optString("id"),event="file."+i;retained.add(key);Item item=items.get(key);
            if(item==null){item=new Item();item.box=host.column();items.put(key,item);root.addView(item.box);}
            JSONObject audio=file.optJSONObject("audio");String audioData=audio==null?"":audio.optJSONObject("file").optString("data");
            if(!file.toString().equals(item.metadata)) {
                boolean preserve=audioData.equals(item.data);if(!preserve)item.releaseAudio();
                item.box.removeAllViews();String label=file.optString("name")+"\n"+file.optString("detail");
                item.box.addView(host.button(label,"native.file."+file.optString("id"),v->emit.accept(event)));
                if(audio!=null) {
                    Item bound=item;
                    Button play=host.button("Play","native.audio.play."+file.optString("id"),v->bound.play(audio));item.play=play;item.box.addView(play);
                    SeekBar seek=new SeekBar(host);seek.setMax(audio.optInt("durationMillis"));GofastrActivity.identify(seek,"native.audio.seek."+file.optString("id"));item.seek=seek;
                    seek.setOnSeekBarChangeListener(new SeekBar.OnSeekBarChangeListener(){public void onProgressChanged(SeekBar bar,int progress,boolean user){if(user && bound.player!=null)bound.player.seekTo(progress);}public void onStartTrackingTouch(SeekBar bar){}public void onStopTrackingTouch(SeekBar bar){}});item.box.addView(seek);
                    if(item.player!=null && item.player.isPlaying())play.setText("Pause");
                }
                item.metadata=file.toString();item.data=audioData;
            }else {
                // Existing listeners use the current event map even when payload
                // content is unchanged and only destinations were refreshed.
                View card=item.box.getChildAt(0);card.setOnClickListener(v->emit.accept(event));
            }
            order(item.box,retained.size()-1);
        }
        JSONObject preview=payload.optJSONObject("preview");
        if(preview!=null) {
            retained.add("preview");Item item=items.get("preview");if(item==null){item=new Item();item.box=host.column();items.put("preview",item);root.addView(item.box);}
            if(!preview.toString().equals(item.metadata)) {
                item.box.removeAllViews();JSONObject file=preview.getJSONObject("file");TextView name=host.text(file.optString("name"),22);item.box.addView(name);item.box.addView(host.text(file.optString("detail"),14));
                if(!preview.optString("caption").isEmpty())item.box.addView(host.text(preview.optString("caption"),16));
                if(!preview.optString("previewTitle").isEmpty())item.box.addView(host.text(preview.optString("previewTitle"),18));
                if(!preview.optString("text").isEmpty()){TextView text=host.text(preview.optString("text"),17);text.setTextIsSelectable(true);GofastrActivity.identify(text,"native.file.preview.text");item.box.addView(text);}
                item.metadata=preview.toString();
            }
            order(item.box,retained.size()-1);
        }
        Iterator<Map.Entry<String,Item>> iterator=items.entrySet().iterator();while(iterator.hasNext()){Map.Entry<String,Item> entry=iterator.next();if(!retained.contains(entry.getKey())){entry.getValue().dispose();root.removeView(entry.getValue().box);iterator.remove();}}
    }
    private void order(View view,int index){if(root.indexOfChild(view)!=index){root.removeView(view);root.addView(view,index);}}
    @Override public void suspend(){for(Item item:items.values())item.releaseAudio();}
    @Override public void dispose(){disposed=true;for(Item item:items.values())item.dispose();items.clear();root.removeAllViews();}
    private final class Item {
        LinearLayout box;ImageView image;Bitmap bitmap;String data="",metadata="";MediaPlayer player;File audioFile;Button play;SeekBar seek;int generation;
        void play(JSONObject audio) {
            if(player!=null){if(player.isPlaying()){player.pause();play.setText("Play");}else{player.start();play.setText("Pause");tick();}return;}
            int token=++generation;play.setEnabled(false);
            host.workers.execute(()->{
                MediaPlayer prepared=null;File file=null;
                try {
                    byte[] bytes=Base64.decode(audio.getJSONObject("file").getString("data"),Base64.DEFAULT);if(bytes.length>1<<20)throw new IOException("Audio too large");
                    file=File.createTempFile("playback-",".audio",host.getCacheDir());try(FileOutputStream output=new FileOutputStream(file)){output.write(bytes);}
                    prepared=new MediaPlayer();prepared.setDataSource(file.getAbsolutePath());prepared.prepare();MediaPlayer ready=prepared;File local=file;
                    host.main.post(()->{
                        if(disposed || token!=generation){ready.release();local.delete();return;}player=ready;audioFile=local;play.setEnabled(true);play.setText("Pause");player.setOnCompletionListener(p->{play.setText("Play");seek.setProgress(0);});player.setOnErrorListener((p,what,extra)->{releaseAudio();return true;});player.start();tick();
                    });
                }catch(Exception e){if(prepared!=null)prepared.release();if(file!=null)file.delete();host.main.post(()->{if(token==generation && play!=null){play.setEnabled(true);play.setText("Audio unavailable");}});}
            });
        }
        void tick(){int token=generation;host.main.postDelayed(()->{if(token!=generation || player==null || disposed)return;try{seek.setProgress(player.getCurrentPosition());if(player.isPlaying())tick();}catch(Exception ignored){}},250);}
        void releaseAudio(){generation++;if(player!=null){player.release();player=null;}if(audioFile!=null){audioFile.delete();audioFile=null;}if(play!=null){play.setEnabled(true);play.setText("Play");}}
        void dispose(){releaseAudio();if(bitmap!=null){if(image!=null)image.setImageDrawable(null);bitmap.recycle();bitmap=null;}}
    }
}
