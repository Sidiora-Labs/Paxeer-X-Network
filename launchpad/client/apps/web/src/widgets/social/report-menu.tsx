'use client';
import { useMemo, useState } from 'react';
import { Flag, MoreHorizontal } from 'lucide-react';
import { gatewayUrl } from '@/core/sdk-config';
import { openAuthModal } from '@/shell/auth-modal-store';
import { DropdownMenu, ModalFrame, type MenuItem } from '@/ui/primitives';
import { accountSession } from '@/widgets/profile/account-transport';
import { ReportContent } from '@/widgets/community-safety/report-content';
import { SafetyClient, type ReportCommand } from '@/widgets/community-safety/safety-client';
import { useViewerWallet } from './use-follow';

export interface ReportSubject {
  targetType: ReportCommand['targetType'];
  targetId: string;
  /** Human description used in the menu's accessible name and the dialog subtitle. */
  label: string;
}

export interface ReportMenuProps {
  subject: ReportSubject;
  /** Additional menu entries rendered above the report action. */
  items?: MenuItem[];
  onLocalMute?: () => void;
  onLocalBlock?: () => void;
  align?: 'start' | 'end';
  size?: 'sm' | 'md';
  className?: string;
}

let safetyClient: SafetyClient | null | undefined;

/** Safety client bound to the media gateway (`POST /v2/safety/reports`), carrying the bearer session. */
function gatewaySafetyClient(): SafetyClient | null {
  if (safetyClient !== undefined) return safetyClient;
  try {
    safetyClient = new SafetyClient(new URL(gatewayUrl('/')).origin, () => accountSession()?.accessToken);
  } catch {
    safetyClient = null;
  }
  return safetyClient;
}

/**
 * Kebab menu with the report action (components.md 14, kebab variant). The
 * report opens the community-safety form in a dialog; the case reference the
 * safety service returns is shown there. Signed out, the action opens sign-in.
 */
export function ReportMenu({ subject, items = [], onLocalMute, onLocalBlock, align = 'end', size = 'sm', className }: ReportMenuProps) {
  const [open, setOpen] = useState(false);
  const viewer = useViewerWallet();
  const client = useMemo(gatewaySafetyClient, []);
  const menu: MenuItem[] = [
    ...items,
    {
      id: 'report',
      label: 'Report',
      icon: <Flag />,
      destructive: true,
      disabled: client === null,
      onSelect: () => {
        if (!viewer) { openAuthModal('Sign in to report'); return; }
        setOpen(true);
      },
    },
  ];
  return (
    <>
      <DropdownMenu label={`More actions for ${subject.label}`} items={menu} trigger={<MoreHorizontal aria-hidden="true" className="size-icon-md" />} align={align} size={size} className={className} />
      {client ? (
        <ModalFrame open={open} onClose={() => setOpen(false)} title="Report" subtitle={`Reporting ${subject.label}`} size="sm">
          <ReportContent client={client} targetType={subject.targetType} targetId={subject.targetId} onLocalMute={onLocalMute} onLocalBlock={onLocalBlock} embedded />
        </ModalFrame>
      ) : null}
    </>
  );
}
