import {
    createClient,
    type AuthChangeEvent,
    type Session,
    type SupabaseClient,
} from '@supabase/supabase-js';
import type { TokenSupplier } from '@paxeer/wallet';
import type { WalletConfig } from './config';

export const IDENTITY_PROVIDERS = ['google', 'discord', 'github', 'apple', 'twitter'] as const;

export type IdentityProvider = (typeof IDENTITY_PROVIDERS)[number];

export interface IdentityUser {
    readonly id: string;
    readonly email: string | null;
}

export class IdentityError extends Error {
    constructor(readonly code: string, message: string) {
        super(message);
        this.name = 'IdentityError';
    }
}

export interface IdentityOptions {
    readonly fetch?: typeof fetch;
    readonly persistSession?: boolean;
    readonly detectSessionInUrl?: boolean;
}

export class IdentitySession {
    constructor(readonly client: SupabaseClient) {}

    static create(config: WalletConfig, options: IdentityOptions = {}): IdentitySession {
        return new IdentitySession(
            createClient(config.identityUrl, config.identityKey, {
                auth: {
                    persistSession: options.persistSession ?? true,
                    autoRefreshToken: options.persistSession ?? true,
                    detectSessionInUrl: options.detectSessionInUrl ?? true,
                },
                global: options.fetch ? { fetch: options.fetch } : undefined,
            }),
        );
    }

    readonly gatewayToken: TokenSupplier = async () => {
        const session = await this.session();
        return session?.access_token ?? null;
    };

    async session(): Promise<Session | null> {
        const { data, error } = await this.client.auth.getSession();
        if (error) throw new IdentityError('session_unavailable', error.message);
        return data.session ?? null;
    }

    async user(): Promise<IdentityUser | null> {
        const session = await this.session();
        if (!session) return null;
        return { id: session.user.id, email: session.user.email ?? null };
    }

    async sendEmailCode(email: string, redirectTo: string): Promise<void> {
        const { error } = await this.client.auth.signInWithOtp({
            email,
            options: { emailRedirectTo: redirectTo },
        });
        if (error) throw new IdentityError('email_sign_in_failed', error.message);
    }

    async verifyEmailCode(email: string, code: string): Promise<IdentityUser> {
        const { data, error } = await this.client.auth.verifyOtp({ email, token: code, type: 'email' });
        if (error) throw new IdentityError('email_code_rejected', error.message);
        const session = data.session;
        if (!session) throw new IdentityError('email_code_rejected', 'the identity provider returned no session');
        return { id: session.user.id, email: session.user.email ?? null };
    }

    async signInWithProvider(provider: IdentityProvider, redirectTo: string): Promise<void> {
        const { error } = await this.client.auth.signInWithOAuth({ provider, options: { redirectTo } });
        if (error) throw new IdentityError('provider_sign_in_failed', error.message);
    }

    async signOut(): Promise<void> {
        const { error } = await this.client.auth.signOut();
        if (error) throw new IdentityError('sign_out_failed', error.message);
    }

    onChange(listener: (event: AuthChangeEvent, session: Session | null) => void): () => void {
        const { data } = this.client.auth.onAuthStateChange(listener);
        return () => data.subscription.unsubscribe();
    }
}
