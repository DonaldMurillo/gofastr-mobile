package dev.gofastr.nativeui;

import android.app.Activity;
import android.Manifest;
import android.content.Context;
import android.content.pm.PackageManager;
import android.hardware.biometrics.BiometricManager;
import android.hardware.biometrics.BiometricPrompt;
import android.hardware.fingerprint.FingerprintManager;
import android.os.Build;
import android.os.CancellationSignal;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import android.security.keystore.KeyPermanentlyInvalidatedException;
import android.util.AtomicFile;
import android.util.Base64;
import android.util.Log;
import org.json.JSONObject;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.security.*;
import java.util.*;
import java.util.concurrent.Executor;
import java.util.function.Consumer;
import javax.crypto.*;
import javax.crypto.spec.GCMParameterSpec;

/** App-private bounded secrets. Biometric reads are bound to Keystore crypto. */
public final class SecureStorage {
    private static final int MAX=16384, MAX_KEYS=128;
    private static final String PREFIX="gofastr.secure.v1.";
    private final Activity activity;
    private final Executor worker;
    private final Executor main;
    private CancellationSignal cancellation;
    private byte[] pendingInput;
    private Consumer<JSONObject> completion;
    private long generation;
    private boolean cancelRequested;

    public SecureStorage(Activity activity, Executor worker) {
        this.activity=activity;this.worker=worker;this.main=activity::runOnUiThread;
    }

    public static boolean biometricAvailable(Context context) {
        if(Build.VERSION.SDK_INT<28 || context.checkSelfPermission(Manifest.permission.USE_BIOMETRIC)!=PackageManager.PERMISSION_GRANTED)return false;
        if(Build.VERSION.SDK_INT>=30) {
            BiometricManager manager=context.getSystemService(BiometricManager.class);
            return manager!=null && manager.canAuthenticate(BiometricManager.Authenticators.BIOMETRIC_STRONG)==BiometricManager.BIOMETRIC_SUCCESS;
        }
        // API 28/29 expose no reliable strong-face availability check. Report
        // the conservative enrolled-fingerprint result; crypto remains final.
        if(Build.VERSION.SDK_INT>=28) {
            FingerprintManager manager=context.getSystemService(FingerprintManager.class);
            return manager!=null && manager.isHardwareDetected() && manager.hasEnrolledFingerprints();
        }
        return false;
    }

    public void perform(JSONObject request, Consumer<JSONObject> callback) {
        if(completion!=null){callback.accept(outcome("failed"));return;}
        completion=callback;cancelRequested=false;long token=++generation;
        try {
            validate(request);
            String op=request.getString("operation");
            if(op.equals("status")){finish(token,success(JSON.object("found",false,"available",true,"biometricAvailable",biometricAvailable(activity))));return;}
            boolean biometric=request.optString("protection").equals("biometric");
            if(biometric && Build.VERSION.SDK_INT<28){finish(token,outcome("unsupported"));return;}
            if(biometric && !op.equals("delete") && !biometricAvailable(activity)){finish(token,outcome("denied"));return;}
            if(op.equals("authenticate")){prompt(token,request.getString("reason"),null,()->finish(token,success(JSON.object("found",false))));return;}
            worker.execute(()->prepare(token,request));
        }catch(Exception error){finish(token,outcome("failed"));}
    }

    private void prepare(long token,JSONObject request) {
        byte[] plaintext=null;
        try {
            String op=request.getString("operation"),protection=request.getString("protection");
            boolean biometric=protection.equals("biometric");
            String identity=identity(protection,request.getString("key")),alias=PREFIX+identity;
            File directory=new File(activity.getNoBackupFilesDir(),"gofastr-secure-v1");
            if(!directory.isDirectory() && !directory.mkdirs())throw new IOException("Private storage unavailable");
            AtomicFile file=new AtomicFile(new File(directory,identity));
            KeyStore store=KeyStore.getInstance("AndroidKeyStore");store.load(null);
            if(op.equals("delete")) {
                // Removing a secret requires no authentication and never reveals it.
                file.delete();if(store.containsAlias(alias))store.deleteEntry(alias);
                main.execute(()->finish(token,success(JSON.object("found",false))));return;
            }
            byte[] stored=null;
            if(op.equals("get")) {
                // openRead recovers AtomicFile's backup after an interrupted
                // write, including API 24's older rename-to-backup format.
                try{stored=read(file.openRead(),MAX*2);}
                catch(FileNotFoundException absent){main.execute(()->finish(token,success(JSON.object("found",false))));return;}
            }
            SecretKey key;
            if(store.containsAlias(alias))key=(SecretKey)store.getKey(alias,null);
            else {
                if(op.equals("get"))throw new GeneralSecurityException("Key unavailable");
                int count=0;Enumeration<String> aliases=store.aliases();while(aliases.hasMoreElements())if(aliases.nextElement().startsWith(PREFIX))count++;
                if(count>=MAX_KEYS)throw new GeneralSecurityException("Secret limit reached");
                KeyGenParameterSpec.Builder spec=new KeyGenParameterSpec.Builder(alias,KeyProperties.PURPOSE_ENCRYPT|KeyProperties.PURPOSE_DECRYPT)
                    .setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).setKeySize(256);
                if(biometric) {
                    spec.setUserAuthenticationRequired(true).setInvalidatedByBiometricEnrollment(true);
                    if(Build.VERSION.SDK_INT>=30)spec.setUserAuthenticationParameters(0,KeyProperties.AUTH_BIOMETRIC_STRONG);
                    else spec.setUserAuthenticationValidityDurationSeconds(-1);
                }
                KeyGenerator generator=KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES,"AndroidKeyStore");generator.init(spec.build());key=generator.generateKey();
            }
            Cipher cipher=Cipher.getInstance("AES/GCM/NoPadding");
            byte[] input;
            if(op.equals("put")) {
                input=Base64.decode(request.getString("value"),Base64.NO_WRAP);plaintext=input;
                cipher.init(Cipher.ENCRYPT_MODE,key);
            } else {
                JSONObject envelope=new JSONObject(new String(stored,StandardCharsets.UTF_8));
                if(envelope.getInt("version")!=1)throw new IOException("Unsupported envelope");
                byte[] iv=Base64.decode(envelope.getString("iv"),Base64.NO_WRAP);
                input=Base64.decode(envelope.getString("ciphertext"),Base64.NO_WRAP);
                if(iv.length!=12 || input.length<17 || input.length>MAX+16)throw new IOException("Invalid envelope");
                cipher.init(Cipher.DECRYPT_MODE,key,new GCMParameterSpec(128,iv));
            }
            byte[] finalInput=input;
            Runnable operation=()->worker.execute(()->crypt(token,op,cipher,finalInput,file,identity));
            if(biometric)main.execute(()->{
                if(token!=generation || completion==null){Arrays.fill(finalInput,(byte)0);return;}
                pendingInput=finalInput;
                prompt(token,request.optString("reason"),cipher,()->{pendingInput=null;operation.run();});
            });
            else operation.run();
            plaintext=null; // crypt owns and clears this byte array.
        } catch(Exception error) {
            diagnose(error);
            if(plaintext!=null)Arrays.fill(plaintext,(byte)0);
            String status=error instanceof KeyPermanentlyInvalidatedException?"denied":"failed";
            main.execute(()->finish(token,outcome(status)));
        }
    }

    private void crypt(long token,String operation,Cipher cipher,byte[] input,AtomicFile file,String identity) {
        byte[] output=null;
        try {
            // Keystore authenticates updateAAD as well as doFinal. Supply AAD
            // only after the CryptoObject's biometric operation is authorized.
            cipher.updateAAD(identity.getBytes(StandardCharsets.US_ASCII));
            output=cipher.doFinal(input);
            if(operation.equals("put")) {
                JSONObject envelope=JSON.object("version",1,"iv",Base64.encodeToString(cipher.getIV(),Base64.NO_WRAP),"ciphertext",Base64.encodeToString(output,Base64.NO_WRAP));
                FileOutputStream stream=null;
                try{stream=file.startWrite();stream.write(envelope.toString().getBytes(StandardCharsets.UTF_8));file.finishWrite(stream);}
                catch(Exception error){if(stream!=null)file.failWrite(stream);throw error;}
                main.execute(()->finish(token,success(JSON.object("found",false))));
            } else {
                if(output.length==0 || output.length>MAX)throw new GeneralSecurityException("Invalid secret size");
                JSONObject result=success(JSON.object("found",true,"value",Base64.encodeToString(output,Base64.NO_WRAP)));
                main.execute(()->finish(token,result));
            }
        }catch(Exception error){diagnose(error);main.execute(()->finish(token,outcome(error instanceof SecurityException?"denied":"failed")));}
        finally{Arrays.fill(input,(byte)0);if(output!=null)Arrays.fill(output,(byte)0);}
    }

    private void prompt(long token,String reason,Cipher cipher,Runnable success) {
        if(completion==null || token!=generation)return;
        if(cancelRequested){finish(token,outcome("cancelled"));return;}
        if(Build.VERSION.SDK_INT<28){finish(token,outcome("unsupported"));return;}
        cancellation=new CancellationSignal();
        try {
            BiometricPrompt.Builder builder=new BiometricPrompt.Builder(activity).setTitle(reason).setNegativeButton("Cancel",main,(dialog,which)->finish(token,outcome("cancelled")));
            if(Build.VERSION.SDK_INT>=30)builder.setAllowedAuthenticators(BiometricManager.Authenticators.BIOMETRIC_STRONG);
            BiometricPrompt.AuthenticationCallback callback=new BiometricPrompt.AuthenticationCallback(){
                @Override public void onAuthenticationSucceeded(BiometricPrompt.AuthenticationResult result){if(token==generation && completion!=null){if(cancelRequested){finish(token,outcome("cancelled"));return;}cancellation=null;success.run();}}
                @Override public void onAuthenticationError(int code,CharSequence text){finish(token,outcome(code==BiometricPrompt.BIOMETRIC_ERROR_USER_CANCELED || code==BiometricPrompt.BIOMETRIC_ERROR_CANCELED?"cancelled":"denied"));}
            };
            BiometricPrompt prompt=builder.build();
            if(cipher==null)prompt.authenticate(cancellation,main,callback);
            else prompt.authenticate(new BiometricPrompt.CryptoObject(cipher),cancellation,main,callback);
        }catch(Exception error){finish(token,outcome("denied"));}
    }

    /** Cancel a prompt. Started storage I/O settles through its actual callback. */
    public void cancel(){cancelRequested=true;if(cancellation!=null)cancellation.cancel();}
    private void finish(long token,JSONObject result) {
        if(token!=generation || completion==null)return;
        if(pendingInput!=null){Arrays.fill(pendingInput,(byte)0);pendingInput=null;}
        Consumer<JSONObject> callback=completion;completion=null;cancellation=null;callback.accept(result);
    }
    private static JSONObject outcome(String status){return JSON.object("status",status);}
    private static void diagnose(Exception error){
        // Log types only. Exception messages can expose identifiers or paths.
        String kind=error.getClass().getSimpleName();
        if(error.getCause()!=null)kind+="/"+error.getCause().getClass().getSimpleName();
        Log.w("gofastr-native","Secure storage failed ("+kind+")");
    }
    private static JSONObject success(JSONObject result){return JSON.object("status","completed","secureStorage",result);}
    private static String identity(String protection,String key)throws Exception {
        byte[] digest=MessageDigest.getInstance("SHA-256").digest((protection+":"+key).getBytes(StandardCharsets.UTF_8));
        StringBuilder value=new StringBuilder();for(byte part:digest)value.append(String.format(Locale.ROOT,"%02x",part&255));return value.toString();
    }
    private static byte[] read(InputStream source,int max)throws IOException {
        try(InputStream input=source;ByteArrayOutputStream output=new ByteArrayOutputStream()) {byte[] buffer=new byte[4096];int count;while((count=input.read(buffer))!=-1){if(output.size()+count>max)throw new IOException("Envelope too large");output.write(buffer,0,count);}return output.toByteArray();}
    }
    public static void validate(JSONObject request)throws Exception {
        String op=request.optString("operation"),key=request.optString("key"),protection=request.optString("protection"),reason=request.optString("reason"),encoded=request.optString("value");
        if(reason.getBytes(StandardCharsets.UTF_8).length>200 || reason.matches("(?s).*[\\p{Cntrl}].*"))throw new IllegalArgumentException("Invalid reason");
        if(op.equals("status")){if(!key.isEmpty() || !protection.isEmpty() || !reason.isEmpty() || !encoded.isEmpty())throw new IllegalArgumentException("Invalid status");return;}
        if(op.equals("authenticate")){if(!key.isEmpty() || !encoded.isEmpty() || !protection.equals("biometric") || reason.trim().isEmpty())throw new IllegalArgumentException("Invalid authentication");return;}
        if(!Arrays.asList("put","get","delete").contains(op) || !key.matches("[A-Za-z0-9][A-Za-z0-9._:-]{0,127}") || !Arrays.asList("device","biometric").contains(protection))throw new IllegalArgumentException("Invalid operation");
        if(op.equals("put")){if(encoded.length()>((MAX+2)/3)*4)throw new IllegalArgumentException("Secret too large");byte[] bytes=Base64.decode(encoded,Base64.NO_WRAP);try{if(bytes.length<1 || bytes.length>MAX)throw new IllegalArgumentException("Invalid secret size");}finally{Arrays.fill(bytes,(byte)0);}}
        else if(!encoded.isEmpty())throw new IllegalArgumentException("Unexpected secret");
        boolean prompt=protection.equals("biometric")&&!op.equals("delete");
        if(prompt?reason.trim().isEmpty():!reason.isEmpty())throw new IllegalArgumentException("Invalid reason");
    }
}
