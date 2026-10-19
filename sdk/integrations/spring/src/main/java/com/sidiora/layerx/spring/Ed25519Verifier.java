package com.sidiora.layerx.spring;

import java.security.GeneralSecurityException;
import java.security.KeyFactory;
import java.security.PublicKey;
import java.security.Signature;
import java.security.spec.X509EncodedKeySpec;
import java.util.HexFormat;

public final class Ed25519Verifier {
    private Ed25519Verifier() {}

    private static final byte[] X509_PREFIX = HexFormat.of().parseHex("302a300506032b6570032100");

    public static boolean verify(byte[] publicKey, byte[] signature, byte[] message) {
        if (publicKey == null || publicKey.length != 32 || signature == null || signature.length != 64
                || message == null) {
            return false;
        }
        try {
            byte[] encoded = new byte[X509_PREFIX.length + publicKey.length];
            System.arraycopy(X509_PREFIX, 0, encoded, 0, X509_PREFIX.length);
            System.arraycopy(publicKey, 0, encoded, X509_PREFIX.length, publicKey.length);
            PublicKey key = KeyFactory.getInstance("Ed25519").generatePublic(new X509EncodedKeySpec(encoded));
            Signature verifier = Signature.getInstance("Ed25519");
            verifier.initVerify(key);
            verifier.update(message);
            return verifier.verify(signature);
        } catch (GeneralSecurityException | RuntimeException error) {
            return false;
        }
    }
}
