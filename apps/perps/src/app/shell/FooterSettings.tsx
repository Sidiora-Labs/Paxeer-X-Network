import React from "react";
import { Pause, Play, Star, List, Ban, type LucideIcon } from "lucide-react";
import { Switch } from "@/ui/atoms/switch";
import { Label } from "@/ui/atoms/label";
import { cn } from "@/ui/utils/cn";
import { Button } from "@/ui/atoms/button";

type TickerDisplayMode = 'none' | 'all' | 'favorites';

interface FooterSettingsProps {
  isPaused: boolean;
  setIsPaused: (value: boolean) => void;
  showPrice: boolean;
  setShowPrice: (value: boolean) => void;
  tickerDisplayMode: TickerDisplayMode;
  setTickerDisplayMode: (value: TickerDisplayMode) => void;
}

export function FooterSettings({ 
  isPaused, 
  setIsPaused, 
  showPrice, 
  setShowPrice, 
  tickerDisplayMode,
  setTickerDisplayMode
}: FooterSettingsProps) {

  const tickerModeOptions: { value: TickerDisplayMode; label: string; icon: LucideIcon }[] = [
    { value: 'none', label: 'None', icon: Ban },
    { value: 'all', label: 'All', icon: List },
    { value: 'favorites', label: 'Favorites', icon: Star },
  ];

  const isDisabled = tickerDisplayMode === 'none';

  return (
    <div className="space-y-3">
      <div className="text-xs font-medium pb-2 text-foreground">Ticker Settings</div>
      
      <div className="space-y-1.5">
        <Label className="text-xs font-medium">Display Mode</Label>
        <div className="flex items-center gap-1.5">
          {tickerModeOptions.map(({ value, label, icon: Icon }) => (
            <Button
              key={value}
              variant={tickerDisplayMode === value ? "secondary" : "ghost"}
              size="sm"
              onClick={() => setTickerDisplayMode(value)}
              className={cn(
                "h-7 px-2 text-xs flex-1 justify-center",
                tickerDisplayMode === value ? "bg-[hsl(var(--muted))] text-[hsl(var(--muted-foreground))]" : "text-muted-foreground"
              )}
            >
              <Icon className={cn("h-3.5 w-3.5 mr-1.5", tickerDisplayMode === value && value === 'favorites' ? "text-[var(--main-accent)]" : "")}/>
              {label}
            </Button>
          ))}
        </div>
      </div>

      <div className={cn("flex items-center justify-between", isDisabled && "opacity-50")}>
        <div className="flex items-center gap-2">
          {isPaused ? <Play className="h-3.5 w-3.5" /> : <Pause className="h-3.5 w-3.5" />}
          <Label htmlFor="pause-ticker" className={cn("text-xs font-medium", !isDisabled && "cursor-pointer")}>
            {isPaused ? "Resume scrolling" : "Pause scrolling"}
          </Label>
        </div>
        <Switch 
          id="pause-ticker" 
          checked={isPaused}
          onCheckedChange={setIsPaused}
          disabled={isDisabled}
          className="data-[state=checked]:bg-[var(--main-accent)]"
        />
      </div>
      
      <div className={cn("flex items-center justify-between", isDisabled && "opacity-50")}>
        <Label htmlFor="show-price" className={cn("text-xs font-medium", !isDisabled && "cursor-pointer")}>
          Show prices
        </Label>
        <Switch 
          id="show-price" 
          checked={showPrice} 
          onCheckedChange={setShowPrice}
          disabled={isDisabled}
          className="data-[state=checked]:bg-[var(--main-accent)]"
        />
      </div>
      
      <div className="pt-1 mt-2 border-t border-border">
        <div className="text-xs text-muted-foreground">
          Additional footer settings will be available soon.
        </div>
      </div>
    </div>
  );
} 