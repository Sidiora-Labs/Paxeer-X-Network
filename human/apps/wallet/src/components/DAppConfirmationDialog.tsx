'use client';

import { ConfirmDrawer } from '@/components/ConfirmDrawer';
import type { DAppConfirmationRequest } from '@/lib/dappConfirm';
import { useLocale } from '@/providers/LocaleProvider';

interface DAppConfirmationDialogProps {
  request: DAppConfirmationRequest | null;
  loading?: boolean;
  onApprove: () => void;
  onReject: () => void;
}

export function DAppConfirmationDialog({
  request,
  loading = false,
  onApprove,
  onReject,
}: DAppConfirmationDialogProps) {
  const { p } = useLocale();
  if (!request) return null;

  return (
    <ConfirmDrawer
      open={!!request}
      onClose={onReject}
      onConfirm={onApprove}
      title={request.title}
      confirmLabel={p.approve}
      cancelLabel={p.reject}
      loading={loading}
    >
      <div className="space-y-3">
        <p className="text-xs leading-relaxed text-pax-muted">{request.description}</p>
        {request.warnings?.map((warning) => (
          <p
            key={warning}
            className="rounded-xl bg-amber-500/10 px-3 py-2 text-xs text-amber-300"
            role="alert"
          >
            {warning}
          </p>
        ))}
        <div className="glass-card max-h-[48vh] overflow-y-auto p-3 space-y-2 text-xs">
          {request.details.map((item) => (
            <div key={item.label} className="space-y-1">
              <p className="text-pax-muted">{item.label}</p>
              <p className="break-all whitespace-pre-wrap font-mono text-white/80">{item.value}</p>
            </div>
          ))}
        </div>
      </div>
    </ConfirmDrawer>
  );
}
