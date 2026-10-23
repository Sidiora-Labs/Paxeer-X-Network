import { ChevronDownIcon, TriangleAlertIcon } from "lucide-react";
import { useState } from "react";

import { ChainIcon } from "@/components/trading/token-icon";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";

function useControllable<T>(value: T | undefined, defaultValue: T, onChange?: (next: T) => void) {
  const [inner, setInner] = useState(defaultValue);
  const current = value !== undefined ? value : inner;
  const set = (next: T) => {
    if (value === undefined) setInner(next);
    onChange?.(next);
  };
  return [current, set] as const;
}

export type ChainOption = {
  id: string;
  name: string;
  iconSrc?: string;
  /** Listed under the Testnet tab. */
  testnet?: boolean;
  disabled?: boolean;
};

export type ChainTab = "mainnet" | "testnet";

export interface ChainListProps {
  chains: ChainOption[];
  /** Selected chain id. */
  value?: string;
  onSelect?: (chain: ChainOption) => void;
  /** Recently used chain ids, shown as quick picks above the mainnet list. */
  recent?: string[];
  tab?: ChainTab;
  defaultTab?: ChainTab;
  onTabChange?: (tab: ChainTab) => void;
  /** Show the "switch to a supported network" tip. */
  wrongNetwork?: boolean;
  /** `compact` uses larger touch rows and fewer columns. */
  variant?: "wide" | "compact";
  className?: string;
}

/** The chain picker body: Mainnet / Testnet tabs, recent picks and a grid of selectable chains. */
export function ChainList({ chains, value, onSelect, recent = [], tab, defaultTab, onTabChange, wrongNetwork, variant = "wide", className }: ChainListProps) {
  const mainnet = chains.filter((c) => !c.testnet);
  const testnet = chains.filter((c) => c.testnet);
  const selected = chains.find((c) => c.id === value);
  const [active, setActive] = useControllable<ChainTab>(tab, defaultTab ?? (selected?.testnet || mainnet.length === 0 ? "testnet" : "mainnet"), onTabChange);
  const recentChains = recent.map((id) => mainnet.find((c) => c.id === id)).filter((c): c is ChainOption => !!c);
  const compact = variant === "compact";

  const grid = (list: ChainOption[], cols: string) => (
    <div className={cn("grid gap-1 rounded-xl", compact && "bg-container p-1", cols)}>
      {list.map((c) => {
        const isSelected = c.id === value;
        return (
          <button
            key={c.id}
            type="button"
            aria-pressed={isSelected}
            disabled={c.disabled}
            onClick={() => onSelect?.(c)}
            className={cn(
              "focus-ring flex items-center gap-2 rounded-lg px-3 text-left text-xs font-medium transition-colors disabled:pointer-events-none disabled:opacity-40",
              compact ? "h-11" : "h-9",
              isSelected ? "bg-container-high" : "hover:bg-container-high/70",
            )}
          >
            <ChainIcon name={c.name} src={c.iconSrc} size={compact ? "sm" : "xs"} />
            <span className="min-w-0 flex-1 truncate">{c.name}</span>
            {isSelected && <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-primary" />}
          </button>
        );
      })}
    </div>
  );

  const mainnetPanel = (
    <div className="flex flex-col gap-3">
      {recentChains.length > 0 && (
        <div className="flex flex-col gap-1.5">
          <span className="text-[11px] text-muted-foreground">Recent</span>
          <div className="flex flex-wrap gap-2">
            {recentChains.map((c) => (
              <button
                key={c.id}
                type="button"
                aria-label={c.name}
                title={c.name}
                onClick={() => onSelect?.(c)}
                className="focus-ring grid size-9 place-items-center rounded-lg border border-border hover:border-primary"
              >
                <ChainIcon name={c.name} src={c.iconSrc} size={compact ? "sm" : "xs"} />
              </button>
            ))}
          </div>
        </div>
      )}
      {grid(mainnet, compact ? "grid-cols-2" : "grid-cols-2 sm:grid-cols-3")}
    </div>
  );
  const testnetPanel = grid(testnet, compact ? "grid-cols-1" : "grid-cols-1 sm:grid-cols-2");

  return (
    <div data-slot="chain-list" className={cn("flex flex-col gap-3", className)}>
      {mainnet.length > 0 && testnet.length > 0 ? (
        <Tabs value={active} onValueChange={(v) => setActive(v as ChainTab)}>
          <TabsList className={compact ? "w-full" : undefined}>
            <TabsTrigger value="mainnet" className={compact ? "flex-1" : undefined}>
              Mainnet
            </TabsTrigger>
            <TabsTrigger value="testnet" className={compact ? "flex-1" : undefined}>
              Testnet
            </TabsTrigger>
          </TabsList>
          <TabsContent value="mainnet">{mainnetPanel}</TabsContent>
          <TabsContent value="testnet">{testnetPanel}</TabsContent>
        </Tabs>
      ) : mainnet.length > 0 ? (
        mainnetPanel
      ) : (
        testnetPanel
      )}
      {wrongNetwork && (
        <p className="flex items-center justify-center gap-1.5 pt-1 text-center text-xs text-amber-600 dark:text-amber-400">
          <TriangleAlertIcon className="size-3.5 shrink-0" />
          Please switch to a supported network to continue.
        </p>
      )}
    </div>
  );
}

export interface ChainSelectorProps extends Omit<ChainListProps, "onSelect" | "value" | "variant" | "className"> {
  value?: string;
  defaultValue?: string;
  onValueChange?: (id: string, chain: ChainOption) => void;
  open?: boolean;
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean) => void;
  /** The trigger turns into a warning "Wrong network" button. */
  wrongNetwork?: boolean;
  /** Show a spinner while the wallet switches. */
  loading?: boolean;
  /** `popover` for headers; `dialog` for mobile bottom bars. */
  layout?: "popover" | "dialog";
  /** Show the chain name next to the icon. */
  showName?: boolean;
  /** Close after a chain is picked. */
  closeOnSelect?: boolean;
  className?: string;
}

/** A trigger showing the current chain that opens a Mainnet / Testnet chain picker. */
export function ChainSelector({
  chains,
  value,
  defaultValue,
  onValueChange,
  open,
  defaultOpen = false,
  onOpenChange,
  wrongNetwork,
  loading,
  layout = "popover",
  showName = false,
  closeOnSelect = true,
  className,
  ...listProps
}: ChainSelectorProps) {
  const [selectedId, setSelectedId] = useState(defaultValue);
  const current = value !== undefined ? value : selectedId;
  const [isOpen, setOpen] = useControllable(open, defaultOpen, onOpenChange);
  const chain = chains.find((c) => c.id === current);

  const select = (c: ChainOption) => {
    if (value === undefined) setSelectedId(c.id);
    onValueChange?.(c.id, c);
    if (closeOnSelect) setOpen(false);
  };

  const triggerClass = cn(
    "focus-ring inline-flex h-9 shrink-0 items-center gap-1.5 rounded-full border px-2.5 text-xs font-medium transition-colors",
    wrongNetwork
      ? "border-amber-500/40 bg-warning-container text-warning hover:bg-warning-container/80"
      : "border-border bg-container hover:bg-container-high data-popup-open:bg-container-high",
    className,
  );
  const triggerBody = (
    <>
      {loading ? (
        <Spinner className="size-4" />
      ) : wrongNetwork ? (
        <TriangleAlertIcon className="size-4" />
      ) : chain ? (
        <ChainIcon name={chain.name} src={chain.iconSrc} size="xs" />
      ) : null}
      {wrongNetwork ? <span>Wrong network</span> : showName && chain ? <span className="max-w-32 truncate">{chain.name}</span> : null}
      <ChevronDownIcon className="size-3.5 opacity-60" />
    </>
  );
  const label = wrongNetwork ? "Wrong network — switch network" : chain ? `Network: ${chain.name}` : "Select network";
  const list = (variant: "wide" | "compact") => <ChainList {...listProps} chains={chains} value={current} onSelect={select} wrongNetwork={wrongNetwork} variant={variant} />;

  if (layout === "dialog") {
    return (
      <>
        <button type="button" aria-label={label} aria-haspopup="dialog" onClick={() => setOpen(true)} className={triggerClass}>
          {triggerBody}
        </button>
        <Dialog open={isOpen} onOpenChange={(o) => setOpen(o)}>
          <DialogContent className="gap-4 p-5 sm:max-w-md">
            <DialogHeader>
              <DialogTitle className="text-base">Switch network</DialogTitle>
            </DialogHeader>
            {list("compact")}
          </DialogContent>
        </Dialog>
      </>
    );
  }

  return (
    <Popover open={isOpen} onOpenChange={(o) => setOpen(o)}>
      <PopoverTrigger aria-label={label} className={triggerClass}>
        {triggerBody}
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[min(28rem,calc(100vw-1.5rem))] rounded-xl p-3">
        {list("wide")}
      </PopoverContent>
    </Popover>
  );
}
