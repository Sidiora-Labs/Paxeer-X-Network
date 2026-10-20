"use client";

import { useTheme } from "next-themes";
import { motion, AnimatePresence } from "framer-motion";
import { 
  XIcon, 
  SunIcon, 
  MoonIcon, 
  Bell, 
  Volume2, 
  Eye, 
  Zap, 
  Shield, 
  BarChart3,
  Check
} from "lucide-react";
import { useState, useEffect } from "react";
import { createPortal } from "react-dom";
import { cn } from "@/ui/utils/cn";
import { useIsMobile } from "@/hooks/ui/useMobile";
import { Switch } from "@/ui/atoms/switch";
import { Label } from "@/ui/atoms/label";
import { Button } from "@/ui/atoms/button";

interface SettingsModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export function SettingsModal({ isOpen, onClose }: SettingsModalProps) {
  const { theme, setTheme, resolvedTheme } = useTheme();
  const [mounted, setMounted] = useState(false);
  const [selected, setSelected] = useState(0);
  const isMobile = useIsMobile();

  // Real settings state
  const [hidePnL, setHidePnL] = useState(false);
  const [hideSize, setHideSize] = useState(false);
  const [showAllWarnings, setShowAllWarnings] = useState(true);
  const [soundForFills, setSoundForFills] = useState(true);
  const [lowPowerMode, setLowPowerMode] = useState(false);
  const [persistConnection, setPersistConnection] = useState(true);
  const [skipConfirmations, setSkipConfirmations] = useState(true);

  // Simple body scroll prevention
  useEffect(() => {
    if (isOpen) {
      document.body.style.overflow = 'hidden';
      return () => {
        document.body.style.overflow = '';
      };
    }
  }, [isOpen]);

  // Handle initial theme setup after mount
  useEffect(() => {
    setMounted(true);
    switch (theme) {
      case 'dark':
        setSelected(1);
        break;
      case 'greenify':
        setSelected(2);
        break;
      case 'hotline':
        setSelected(3);
        break;
      case 'oled':
        setSelected(4);
        break;
      default:
        setSelected(0);
    }
  }, [theme]);

  const themeOptions = [
   {
      label: "Paxify",
      icon: SunIcon,
      value: 'greenify',
      // Real backdrop served from /public — shown as the preview.
      bgImage: '/Paxify.png',
      gradient: 'linear-gradient(135deg, var(--brand-blue) 0%, #0035a8 100%)',
      badgeColor: '#ecfdf5',
      badgeBg: '#13136d',
      border: '240 70% 25%'
    },
    {
      label: "Dark",
      icon: MoonIcon,
      value: 'dark',
      bgImage: '/Dark.png',
      gradient: 'linear-gradient(135deg, #0f172a 0%, #1e293b 50%, #334155 100%)',
      badgeColor: '#f1f5f9',
      badgeBg: '#1e293b',
      border: '#334155'
    },
    {
      label: "Hotline",
      icon: MoonIcon,
      value: 'hotline',
      bgImage: '/Hotline.png',
      gradient: 'linear-gradient(135deg, #701a75 0%, #be185d 50%, #ec4899 100%)',
      badgeColor: '#fdf2f8',
      badgeBg: '#701a75',
      border: '#be185d'
    },
    {
      label: "OLED",
      icon: MoonIcon,
      value: 'oled',
      bgImage: '/OLED.png',
      gradient: 'linear-gradient(135deg, #000000 0%, var(--brand-card) 50%, #1a1a1a 100%)',
      badgeColor: '#ffffff',
      badgeBg: '#000000',
      border: '#333333'
    },
  ];

  const handleThemeChange = (index: number) => {
    const newTheme = themeOptions[index].value;
    setSelected(index);
    setTheme(newTheme);
  };

  // Don't render anything until mounted to prevent hydration mismatch
  if (!mounted) return null;

  const modalContent = (
    <AnimatePresence>
      {isOpen && (
        <>
          {/* Backdrop */}
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            onClick={(e) => {
              if (e.target === e.currentTarget) {
                onClose();
              }
            }}
            className="fixed inset-0 z-[9998] bg-black/50 backdrop-blur-sm"
          />

          {/* Drawer */}
          <motion.div
            initial={isMobile ? { y: "100%" } : { x: "100%" }}
            animate={isMobile ? { y: 0 } : { x: 0 }}
            exit={isMobile ? { y: "100%" } : { x: "100%" }}
            transition={{
              type: "spring",
              damping: 30,
              stiffness: 300,
            }}
            className={cn(
              "fixed z-[9999] bg-card bg-surface-1 shadow-2xl flex flex-col",
              isMobile 
                ? "bottom-0 left-0 right-0 rounded-t-2xl h-[85vh]" 
                : "top-0 right-0 h-screen w-[420px] max-w-[90vw]"
            )}
          >
            {/* Header */}
            <div className="flex items-center justify-between p-6 border-b border-border flex-shrink-0">
              <div>
                <h2 className="text-lg font-semibold text-foreground">
                  Settings
                </h2>
                <p className="text-sm text-muted-foreground mt-0.5">
                  Customize your experience
                </p>
              </div>
              <Button
                variant="ghost"
                size="icon"
                onClick={onClose}
                className="h-8 w-8 text-muted-foreground hover:text-foreground"
              >
                <XIcon className="w-4 h-4" />
              </Button>
            </div>

            {/* Scrollable Content */}
            <div 
              className={cn(
                "flex-1 overflow-y-scroll",
                isMobile ? "" : "scrollbar-custom"
              )}
              style={{
                WebkitOverflowScrolling: 'touch'
              }}
            >
              <div className="p-6 space-y-6">
                {/* Theme Section */}
                <div className="space-y-4">
                  <div>
                    <h3 className="text-sm font-medium text-foreground mb-1">
                      Appearance
                    </h3>
                    <p className="text-xs text-muted-foreground">
                      Choose your preferred theme
                    </p>
                  </div>
                  <div className="grid grid-cols-1 gap-3">
                    {themeOptions.map((option, index) => (
                      <button
                        key={option.value}
                        onClick={() => handleThemeChange(index)}
                        className={cn(
                          // Apple-style interactive card. Adds the
                          // specular hairline + hover lift + sweep on
                          // top of the existing selected-ring affordance.
                          "au-card au-card--interactive",
                          "flex items-center gap-4 !p-3 rounded-xl !border-2 transition-all duration-200",
                          index === selected
                            ? "!border-accent bg-accent/10 ring-2 ring-accent/20"
                            : "!border-border hover:!border-accent/50 hover:!bg-accent/5"
                        )}
                      >
                        {/* Real backdrop preview — mirrors what the
                            user will see across the app once selected. */}
                        <div
                          className="w-10 h-10 rounded-lg border flex items-center justify-center relative overflow-hidden shrink-0"
                          style={{
                            backgroundImage: `url(${option.bgImage}), ${option.gradient}`,
                            backgroundSize: 'cover',
                            backgroundPosition: 'center',
                            borderColor: option.border
                          }}
                        >
                          {index === selected && (
                            <Check
                              className="w-5 h-5 text-white drop-shadow-sm"
                              style={{ color: option.badgeColor }}
                            />
                          )}
                        </div>

                        {/* Theme Info */}
                        <div className="flex-1 text-left">
                          <div className="text-sm font-medium text-foreground">
                            {option.label}
                          </div>
                          <div className="text-xs text-muted-foreground">
                            {option.label === 'Paxify' && 'Fresh and natural'}
                            {option.label === 'Dark' && 'Easy on the eyes'}
                            {option.label === 'Hotline' && 'Bold and vibrant'}
                            {option.label === 'OLED' && 'Pure black theme'}
                          </div>
                        </div>

                        {/* Icon */}
                        <option.icon className="w-4 h-4 text-muted-foreground" />
                      </button>
                    ))}
                  </div>
                </div>

                {/* Display & Privacy Section */}
                <div className="space-y-4">
                  <div>
                    <h3 className="text-sm font-medium text-foreground mb-1">
                      Display & Privacy
                    </h3>
                    <p className="text-xs text-muted-foreground">
                      Control what information is displayed
                    </p>
                  </div>
                  
                  <div className="space-y-4">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-3">
                        <div className="p-2 rounded-lg bg-accent/10">
                          <Eye className="h-4 w-4 text-accent" />
                        </div>
                        <div>
                          <Label htmlFor="hide-pnl" className="text-sm font-medium cursor-pointer">
                            Hide PnL
                          </Label>
                          <p className="text-xs text-muted-foreground">
                            Hide profit and loss information
                          </p>
                        </div>
                      </div>
                      <Switch 
                        id="hide-pnl" 
                        checked={hidePnL}
                        onCheckedChange={setHidePnL}
                        className="data-[state=checked]:border-accent"
                        style={{
                          backgroundColor: hidePnL ? 'var(--main-accent)' : undefined
                        }}
                      />
                    </div>

                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-3">
                        <div className="p-2 rounded-lg bg-accent/10">
                          <Shield className="h-4 w-4 text-accent" />
                        </div>
                        <div>
                          <Label htmlFor="hide-size" className="text-sm font-medium cursor-pointer">
                            Hide Size/Margin
                          </Label>
                          <p className="text-xs text-muted-foreground">
                            Hide position size and margin info
                          </p>
                        </div>
                      </div>
                      <Switch 
                        id="hide-size" 
                        checked={hideSize}
                        onCheckedChange={setHideSize}
                        className="data-[state=checked]:border-accent"
                        style={{
                          backgroundColor: hideSize ? 'var(--main-accent)' : undefined
                        }}
                      />
                    </div>

                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-3">
                        <div className="p-2 rounded-lg bg-accent/10">
                          <Bell className="h-4 w-4 text-accent" />
                        </div>
                        <div>
                          <Label htmlFor="show-warnings" className="text-sm font-medium cursor-pointer">
                            Show All Warnings
                          </Label>
                          <p className="text-xs text-muted-foreground">
                            Display all risk and safety warnings
                          </p>
                        </div>
                      </div>
                      <Switch 
                        id="show-warnings" 
                        checked={showAllWarnings}
                        onCheckedChange={setShowAllWarnings}
                        className="data-[state=checked]:border-accent"
                        style={{
                          backgroundColor: showAllWarnings ? 'var(--main-accent)' : undefined
                        }}
                      />
                    </div>
                  </div>
                </div>

                {/* Audio & Notifications Section */}
                <div className="space-y-4">
                  <div>
                    <h3 className="text-sm font-medium text-foreground mb-1">
                      Audio & Notifications
                    </h3>
                    <p className="text-xs text-muted-foreground">
                      Configure audio feedback
                    </p>
                  </div>
                  
                  <div className="space-y-4">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-3">
                        <div className="p-2 rounded-lg bg-accent/10">
                          <Volume2 className="h-4 w-4 text-accent" />
                        </div>
                        <div>
                          <Label htmlFor="sound-fills" className="text-sm font-medium cursor-pointer">
                            Sound for Fills/Liquidations
                          </Label>
                          <p className="text-xs text-muted-foreground">
                            Play audio alerts for order fills and liquidations
                          </p>
                        </div>
                      </div>
                      <Switch 
                        id="sound-fills" 
                        checked={soundForFills}
                        onCheckedChange={setSoundForFills}
                        className="data-[state=checked]:border-accent"
                        style={{
                          backgroundColor: soundForFills ? 'var(--main-accent)' : undefined
                        }}
                      />
                    </div>
                  </div>
                </div>

                {/* Performance Section */}
                <div className="space-y-4">
                  <div>
                    <h3 className="text-sm font-medium text-foreground mb-1">
                      Performance
                    </h3>
                    <p className="text-xs text-muted-foreground">
                      Optimize app performance
                    </p>
                  </div>
                  
                  <div className="space-y-4">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-3">
                        <div className="p-2 rounded-lg bg-accent/10">
                          <Zap className="h-4 w-4 text-accent" />
                        </div>
                        <div>
                          <Label htmlFor="low-power" className="text-sm font-medium cursor-pointer">
                            Low Power Mode
                          </Label>
                          <p className="text-xs text-muted-foreground">
                            Disable CPU intensive animations
                          </p>
                        </div>
                      </div>
                      <Switch 
                        id="low-power" 
                        checked={lowPowerMode}
                        onCheckedChange={setLowPowerMode}
                        className="data-[state=checked]:border-accent"
                        style={{
                          backgroundColor: lowPowerMode ? 'var(--main-accent)' : undefined
                        }}
                      />
                    </div>
                  </div>
                </div>

                {/* Trading Section */}
                <div className="space-y-4">
                  <div>
                    <h3 className="text-sm font-medium text-foreground mb-1">
                      Trading
                    </h3>
                    <p className="text-xs text-muted-foreground">
                      Trading experience preferences
                    </p>
                  </div>
                  
                  <div className="space-y-4">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-3">
                        <div className="p-2 rounded-lg bg-accent/10">
                          <Shield className="h-4 w-4 text-accent" />
                        </div>
                        <div>
                          <Label htmlFor="persist-connection" className="text-sm font-medium cursor-pointer">
                            Persist Trading Connection
                          </Label>
                          <p className="text-xs text-muted-foreground">
                            Keep wallet connected on page refresh
                          </p>
                        </div>
                      </div>
                      <Switch 
                        id="persist-connection" 
                        checked={persistConnection}
                        onCheckedChange={setPersistConnection}
                        className="data-[state=checked]:border-accent"
                        style={{
                          backgroundColor: persistConnection ? 'var(--main-accent)' : undefined
                        }}
                      />
                    </div>

                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-3">
                        <div className="p-2 rounded-lg bg-accent/10">
                          <BarChart3 className="h-4 w-4 text-accent" />
                        </div>
                        <div>
                          <Label htmlFor="skip-confirmations" className="text-sm font-medium cursor-pointer">
                            Skip Order Confirmations
                          </Label>
                          <p className="text-xs text-muted-foreground">
                            Execute orders without confirmation dialogs
                          </p>
                        </div>
                      </div>
                      <Switch 
                        id="skip-confirmations" 
                        checked={skipConfirmations}
                        onCheckedChange={setSkipConfirmations}
                        className="data-[state=checked]:border-accent"
                        style={{
                          backgroundColor: skipConfirmations ? 'var(--main-accent)' : undefined
                        }}
                      />
                    </div>
                  </div>
                </div>

                {/* Footer */}
                <div className="border-t border-border pt-4 pb-2">
                  <div className="text-xs text-muted-foreground text-center">
                    Settings are automatically saved
                  </div>
                </div>
              </div>
            </div>
          </motion.div>
        </>
      )}
    </AnimatePresence>
  );

  // Use portal to render outside the header hierarchy
  return typeof window !== 'undefined' ? createPortal(modalContent, document.body) : null;
} 