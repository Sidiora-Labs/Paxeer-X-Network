'use client';

import { SvgIcon } from '@/components/ui/SvgIcon';
import { Loader2 } from 'lucide-react';

interface ImportPkViewProps {
  importPk: string;
  setImportPk: (v: string) => void;
  importPkName: string;
  setImportPkName: (v: string) => void;
  importPkError: string;
  importPkLoading: boolean;
  accountCount: number;
  onImport: () => void;
  onBack: () => void;
}

export function ImportPkView({
  importPk, setImportPk, importPkName, setImportPkName,
  importPkError, importPkLoading, accountCount, onImport, onBack,
}: ImportPkViewProps) {
  return (
    <div className="px-4 pt-4 pb-24">
      <div className="flex items-center gap-3 mb-6">
        <button onClick={onBack} className="p-2 -ml-2 press-scale">
          <SvgIcon name="arrow-left" className="w-5 h-5" style={{ filter: 'brightness(0) invert(0.6)' }} />
        </button>
        <h2 className="text-lg font-bold">Import Private Key</h2>
      </div>

      <div className="glass-card p-4 mb-4">
        <div className="flex items-start gap-2 mb-3">
          <SvgIcon name="warning" className="w-4 h-4" style={{ filter: 'invert(78%) sepia(64%) saturate(1000%) hue-rotate(360deg) brightness(100%)' }} />
          <p className="text-xs text-amber-400/80">
            This imports a single account by private key. The imported account will appear alongside your HD-derived accounts.
          </p>
        </div>
      </div>

      <div className="space-y-4">
        <div>
          <label className="text-xs text-pax-muted mb-1.5 block">Account Name (optional)</label>
          <input
            type="text"
            value={importPkName}
            onChange={(e) => setImportPkName(e.target.value)}
            placeholder={`Imported ${accountCount + 1}`}
            className="w-full px-4 py-3.5 rounded-xl bg-white/5   text-sm outline-none  transition-colors placeholder:text-white/20"
          />
        </div>

        <div>
          <label className="text-xs text-pax-muted mb-1.5 block">Private Key</label>
          <textarea
            value={importPk}
            onChange={(e) => setImportPk(e.target.value)}
            placeholder="0x..."
            rows={3}
            className="w-full px-4 py-3 rounded-xl bg-white/5   text-sm outline-none  transition-colors placeholder:text-white/20 resize-none font-mono"
          />
        </div>

        {importPkError && <p className="text-red-400 text-xs">{importPkError}</p>}

        <button
          onClick={onImport}
          disabled={importPkLoading || !importPk.trim()}
          className="w-full flex items-center justify-center gap-2 py-3.5 rounded-2xl bg-pax-accent text-black font-semibold text-sm press-scale disabled:opacity-40 transition-all"
        >
          {importPkLoading ? (
            <Loader2
              aria-label="Importing account"
              className="h-5 w-5 animate-spin"
            />
          ) : (
            <>
              <SvgIcon name="arrow-left" className="w-4 h-4 rotate-90" style={{ filter: 'brightness(0)' }} />
              Import Account
            </>
          )}
        </button>
      </div>
    </div>
  );
}
