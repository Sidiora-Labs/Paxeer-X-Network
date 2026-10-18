'use client';

import { Toaster } from 'sonner';

export default function ToastProvider() {
  return (
    <Toaster
      position="bottom-center"
      toastOptions={{
        style: {
          background: 'var(--sl-surface-3)',
          color: 'var(--sl-text)',
          borderWidth: 0,
          fontSize: '13px',
          fontFamily: 'var(--sl-font-sans)',
          borderRadius: '12px',
          boxShadow: 'var(--sl-shadow-pop)',
          maxWidth: 'calc(100vw - 32px)',
        },
        classNames: {
          success: '[&>div>svg]:text-accent',
          error: '[&>div>svg]:text-danger',
          warning: '[&>div>svg]:text-warning',
          info: '[&>div>svg]:text-info',
        },
      }}
      gap={8}
      expand={false}
      richColors={false}
      closeButton
      mobileOffset={88}
    />
  );
}
