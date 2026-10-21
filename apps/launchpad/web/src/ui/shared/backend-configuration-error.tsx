import { TriangleAlert } from 'lucide-react';
import { plainCopy } from '@/core/user-facing-error';
import type { BackendOriginError } from '@/core/backend-origins';

/**
 * Full-page state the root layout renders instead of the app when the
 * deployment's backend origins are missing or unusable
 * (core/backend-origins.ts backendConfigurationProblems()). Nothing that would
 * open a backend connection is mounted. Body text is the plain copy; the
 * variable names are never rendered, the server log carries them.
 */
export function BackendConfigurationError({ problems }: { problems: readonly BackendOriginError[] }) {
  void problems;
  return (
    <main
      role="alert"
      data-state="backend-configuration-error"
      className="flex min-h-[70vh] flex-col items-center justify-center gap-6 p-8"
    >
      <div className="flex h-20 w-20 items-center justify-center rounded-lg bg-danger-soft text-danger">
        <TriangleAlert size={36} aria-hidden="true" />
      </div>
      <div className="space-y-2 text-center">
        <h1 className="text-xl font-bold text-text">Sidiora can't load right now</h1>
        <p className="max-w-sm text-sm text-text-muted">{plainCopy.configuration}</p>
        <p className="max-w-sm text-sm text-text-muted">Your funds and data are not affected.</p>
      </div>
    </main>
  );
}
