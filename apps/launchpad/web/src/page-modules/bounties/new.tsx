'use client';
import Link from 'next/link';
import { ArrowLeft } from 'lucide-react';
import { CreateBountyForm } from '@/widgets/bounties/create-bounty-form';

export default function NewBountyPage() {
  return (
    <div className="mx-auto flex w-full max-w-modal-width-lg flex-col gap-6 px-4 py-6">
      <Link href="/bounties" className="inline-flex w-fit items-center gap-1 rounded-sm text-sm text-text-muted hover:text-text focus-visible:outline-2 focus-visible:outline-focus">
        <ArrowLeft aria-hidden="true" className="size-icon-sm" />All bounties
      </Link>
      <header className="flex flex-col gap-1">
        <p className="text-2xs font-bold uppercase tracking-[.04em] text-text-faint">Bounties</p>
        <h1 className="text-2xl font-extrabold text-text">Create a bounty</h1>
        <p className="text-sm text-text-muted">Post a task, lock in a reward, then pick the winners yourself</p>
      </header>
      <CreateBountyForm />
    </div>
  );
}
