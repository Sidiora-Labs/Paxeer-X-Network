'use client';

import { type ReactNode } from 'react';
import { Wallet } from 'lucide-react';
import { useAccount } from 'wagmi';
import { Button, EmptyState } from '@/ui/primitives';
import { useAuthModal } from '@/shell/auth-modal-store';
import SignInModal from '@/shell/sign-in-modal';

interface WalletGateProps {
  children: ReactNode;
  message?: string;
}

/** Blocks a page until a wallet is connected; the call to action opens the shared sign-in dialog. */
export default function WalletGate({ children, message = 'Connect your wallet to continue' }: WalletGateProps) {
  const { isConnected } = useAccount();
  const { openModal } = useAuthModal();

  if (!isConnected) {
    return (
      <div className="flex min-h-[60vh] items-center justify-center px-4">
        <EmptyState icon={Wallet} title={message} body="No account or password needed"
          action={<Button variant="primary" onClick={() => openModal(message)}>Connect wallet</Button>} />
        <SignInModal />
      </div>
    );
  }

  return <>{children}</>;
}
