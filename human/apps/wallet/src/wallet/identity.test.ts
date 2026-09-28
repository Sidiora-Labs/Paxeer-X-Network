import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { IdentityError, IdentitySession } from './identity';
import { ACCESS_TOKEN, EMAIL, EMAIL_CODE, startGateway, type TestGateway } from './test/gateway';

let gateway: TestGateway;

beforeEach(async () => {
    gateway = await startGateway();
});

afterEach(async () => {
    await gateway.close();
});

function session(): IdentitySession {
    return IdentitySession.create(gateway.config, { persistSession: false, detectSessionInUrl: false });
}

describe('identity session', () => {
    it('wallet_identity_email_code_sign_in_makes_the_access_token_the_gateway_bearer', async () => {
        const identity = session();
        expect(await identity.gatewayToken()).toBeNull();
        await identity.sendEmailCode(EMAIL, 'http://127.0.0.1:3000/auth/callback');
        expect(gateway.requests.some((r) => r.path === '/auth/v1/otp')).toBe(true);
        const user = await identity.verifyEmailCode(EMAIL, EMAIL_CODE);
        expect(user.email).toBe(EMAIL);
        expect(await identity.gatewayToken()).toBe(ACCESS_TOKEN);
        expect((await identity.user())?.email).toBe(EMAIL);
        await identity.signOut();
        expect(await identity.gatewayToken()).toBeNull();
    });

    it('wallet_identity_rejects_a_wrong_email_code', async () => {
        const identity = session();
        await expect(identity.verifyEmailCode(EMAIL, '000000')).rejects.toBeInstanceOf(IdentityError);
        expect(await identity.gatewayToken()).toBeNull();
    });
});
