package com.sidiora.layerx.android.sample;

import android.app.Activity;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.widget.Button;
import android.widget.TextView;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.sidiora.layerx.android.MobileIntegrationException;
import com.sidiora.layerx.android.ReceiptGate;
import com.sidiora.layerx.sdk.IdempotencyKey;
import com.sidiora.layerx.sdk.ProtocolAmount;
import java.io.IOException;
import java.math.BigInteger;
import java.util.UUID;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/** The wallet screen: nothing is shown as settled until the device verifies the receipt. */
public final class WalletActivity extends Activity {
    private final ExecutorService worker = Executors.newSingleThreadExecutor();
    private final Handler main = new Handler(Looper.getMainLooper());

    private TextView service;
    private TextView identity;
    private TextView activity;
    private TextView settlement;
    private TextView deliveries;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        setContentView(R.layout.activity_wallet);
        service = findViewById(R.id.service);
        identity = findViewById(R.id.identity);
        activity = findViewById(R.id.activity);
        settlement = findViewById(R.id.settlement);
        deliveries = findViewById(R.id.deliveries);

        Button refresh = findViewById(R.id.refresh);
        Button pay = findViewById(R.id.pay);
        refresh.setOnClickListener(view -> submit(() -> LayerXHolder.shared(this).model().refresh()));
        pay.setOnClickListener(view -> submit(this::pay));
        submit(() -> LayerXHolder.shared(this).model().refresh());
    }

    @Override
    protected void onResume() {
        super.onResume();
        render(LayerXHolder.shared(this).model().current());
    }

    @Override
    protected void onDestroy() {
        worker.shutdownNow();
        super.onDestroy();
    }

    private WalletModel.Snapshot pay() {
        WalletModel model = LayerXHolder.shared(this).model();
        ReceiptGate.Expectation expectation = new ReceiptGate.Expectation(
            RelayReceiptResolver.hex32(getString(R.string.layerx_sample_asset)),
            RelayReceiptResolver.hex32(getString(R.string.layerx_sample_recipient)),
            amount(getString(R.string.layerx_sample_amount)));
        JsonNode request = quote(getString(R.string.layerx_sample_quote_json));
        WalletModel.Snapshot paid = model.pay(request, expectation,
            new IdempotencyKey(UUID.randomUUID().toString()));
        if (paid.settlement() instanceof ReceiptGate.Pending pending) {
            return model.awaitSettlement(pending.reference(), expectation, 20, Thread::sleep);
        }
        return paid;
    }

    private void submit(java.util.function.Supplier<WalletModel.Snapshot> work) {
        worker.execute(() -> {
            WalletModel.Snapshot snapshot;
            try {
                snapshot = work.get();
            } catch (MobileIntegrationException error) {
                main.post(() -> settlement.setText(getString(R.string.label_settlement) + ": " + error.code().wire()));
                return;
            }
            main.post(() -> render(snapshot));
        });
    }

    private void render(WalletModel.Snapshot snapshot) {
        service.setText(getString(R.string.label_service) + ": " + text(snapshot.serviceVersion()));
        identity.setText(getString(R.string.label_identity) + ": " + text(snapshot.displayName()));
        activity.setText(getString(R.string.label_activity) + ": " + snapshot.activityCount());
        settlement.setText(getString(R.string.label_settlement) + ": " + describe(snapshot));
        deliveries.setText(getString(R.string.label_deliveries) + ": "
            + (snapshot.deliveries().isEmpty() ? getString(R.string.state_idle)
               : String.join(", ", snapshot.deliveries())));
    }

    private String describe(WalletModel.Snapshot snapshot) {
        if (snapshot.refusal() != null) return snapshot.refusal();
        ReceiptGate.State state = snapshot.settlement();
        if (state instanceof ReceiptGate.Verified verified) {
            return verified.level() + " " + verified.receiptDigest();
        }
        if (state instanceof ReceiptGate.Refused refused) return refused.code();
        if (state instanceof ReceiptGate.Pending pending) return "pending " + pending.reference();
        return getString(R.string.state_idle);
    }

    private String text(String value) {
        return value == null || value.isEmpty() ? getString(R.string.state_idle) : value;
    }

    private static ProtocolAmount amount(String value) {
        if (value.isEmpty()) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_CONFIGURATION);
        }
        for (int index = 0; index < value.length(); index++) {
            if (value.charAt(index) < '0' || value.charAt(index) > '9') {
                throw MobileIntegrationException.of(MobileIntegrationException.Code.INVALID_CONFIGURATION);
            }
        }
        return new ProtocolAmount(new BigInteger(value));
    }

    private static JsonNode quote(String value) {
        try {
            JsonNode parsed = new ObjectMapper().readTree(value);
            if (parsed == null || !parsed.isObject()) {
                throw MobileIntegrationException.of(MobileIntegrationException.Code.DECODE_FAILURE);
            }
            return parsed;
        } catch (IOException error) {
            throw MobileIntegrationException.of(MobileIntegrationException.Code.DECODE_FAILURE);
        }
    }
}
