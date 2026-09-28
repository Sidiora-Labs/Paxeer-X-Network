'use client';

import Image from 'next/image';

interface ProviderButtonProps {
  label: string;
  iconSrc: string;
  onClick: () => void;
  loading?: boolean;
  disabled?: boolean;
}

/**
 * One row in the OAuth provider stack. Monochrome surface, neutral border.
 * No accent color here — primary blue is reserved for the user's chosen action
 * (the "Send magic link" CTA, "Send Transaction" CTA, etc.).
 */
export function ProviderButton({
  label,
  iconSrc,
  onClick,
  loading,
  disabled,
}: ProviderButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={loading || disabled}
      className="
        group flex w-full items-center gap-3 rounded-xl
        border border-neutral-700 bg-neutral-800
        px-4 py-3 text-left text-[15px] text-neutral-100
        transition-[background,border,transform]
        duration-[var(--duration-snappy)]
        ease-[var(--ease-standard)]
        hover:border-neutral-600 hover:bg-neutral-700
        active:scale-[0.99]
        disabled:cursor-not-allowed disabled:opacity-50
      "
    >
      <span className="flex h-6 w-6 items-center justify-center">
        <Image src={iconSrc} alt="" width={24} height={24} className="h-6 w-6" />
      </span>
      <span className="flex-1">{label}</span>
      {loading && <Spinner />}
    </button>
  );
}

function Spinner() {
  return (
    <svg
      className="h-4 w-4 animate-spin text-neutral-400"
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
    >
      <circle
        className="opacity-25"
        cx="12"
        cy="12"
        r="10"
        stroke="currentColor"
        strokeWidth="3"
      />
      <path
        className="opacity-75"
        fill="currentColor"
        d="M4 12a8 8 0 018-8v3a5 5 0 00-5 5H4z"
      />
    </svg>
  );
}
