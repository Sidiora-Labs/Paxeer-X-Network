package com.sidiora.layerx.android.sample;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.util.Log;
import com.sidiora.layerx.android.EventEnvelopeHeaders;
import com.sidiora.layerx.android.MobileIntegrationException;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/** Relayed events reach the wallet only after Ed25519 verification and delivery-claim replay protection. */
public final class EventRelayReceiver extends BroadcastReceiver {
    public static final String ACTION = "com.sidiora.layerx.android.sample.EVENT";
    public static final String EXTRA_BODY = "layerx.body";
    public static final String EXTRA_DELIVERY_ID = "layerx.delivery_id";
    public static final String EXTRA_TIMESTAMP = "layerx.timestamp";
    public static final String EXTRA_KEY_ID = "layerx.key_id";
    public static final String EXTRA_SIGNATURE = "layerx.signature";

    private static final String TAG = "LayerXEventRelay";
    private static final ExecutorService WORKER = Executors.newSingleThreadExecutor();

    @Override
    public void onReceive(Context context, Intent intent) {
        if (intent == null || !ACTION.equals(intent.getAction())) return;
        byte[] body = intent.getByteArrayExtra(EXTRA_BODY);
        String deliveryId = intent.getStringExtra(EXTRA_DELIVERY_ID);
        String timestamp = intent.getStringExtra(EXTRA_TIMESTAMP);
        String keyId = intent.getStringExtra(EXTRA_KEY_ID);
        String signature = intent.getStringExtra(EXTRA_SIGNATURE);
        if (body == null || deliveryId == null || timestamp == null || keyId == null || signature == null) {
            Log.w(TAG, "rejected: incomplete delivery envelope");
            return;
        }
        Map<String, String> headers = new HashMap<>();
        headers.put(EventEnvelopeHeaders.ID_HEADER, deliveryId);
        headers.put(EventEnvelopeHeaders.TIMESTAMP_HEADER, timestamp);
        headers.put(EventEnvelopeHeaders.KEY_ID_HEADER, keyId);
        headers.put(EventEnvelopeHeaders.SIGNATURE_HEADER, signature);

        Context application = context.getApplicationContext();
        BroadcastReceiver.PendingResult pending = goAsync();
        WORKER.execute(() -> {
            try {
                WalletModel.Snapshot snapshot = LayerXHolder.shared(application).model().deliver(body, headers);
                if (snapshot.refusal() != null) Log.w(TAG, "rejected: " + snapshot.refusal());
            } catch (MobileIntegrationException error) {
                Log.w(TAG, "rejected: " + error.code().wire());
            } finally {
                pending.finish();
            }
        });
    }
}
