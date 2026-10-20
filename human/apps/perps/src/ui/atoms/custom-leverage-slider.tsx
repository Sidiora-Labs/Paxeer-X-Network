"use client"

import type React from "react"
import { useState, useRef, useEffect } from "react"
import { cn } from "@/ui/utils/cn"

interface CustomLeverageSliderProps {
  min?: number
  max?: number
  step?: number
  defaultValue?: number
  value?: number
  onChange?: (value: number) => void
  className?: string
}

export default function CustomLeverageSlider({
  min = 1,
  max = 100,
  step = 1,
  defaultValue = 10,
  value,
  onChange,
  className,
}: CustomLeverageSliderProps) {
  const [internalValue, setInternalValue] = useState(defaultValue)
  const [isDragging, setIsDragging] = useState(false)
  const trackRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (value !== undefined) {
      setInternalValue(value)
    }
  }, [value])

  const currentValue = value !== undefined ? value : internalValue
  
  // Define a subtle edge padding (3% on each side)
  const edgePadding = 0.03
  
  const percentage = ((currentValue - min) / (max - min)) * 100

  // Generate better key levels
  const generateNiceKeyLevels = (min: number, max: number) => {
    // Always include min and max
    const result: number[] = [min, max]
    
    // Find appropriate midpoints
    if (max <= 10) {
      // For small ranges (1-10), use 5 as midpoint
      result.push(5)
    } else if (max <= 20) {
      // For medium ranges (1-20), use 10 as midpoint
      result.push(10)
    } else if (max <= 50) {
      // For larger ranges (1-50), use 25 as midpoint
      result.push(25)
    } else if (max <= 100) {
      // For ranges up to 100, use 50 as midpoint
      result.push(50)
    } else if (max <= 200) {
      // For ranges up to 200, use 50 and 100 as midpoints
      result.push(50, 100)
    } else {
      // For very large ranges, use multiple nice round numbers
      const step = Math.pow(10, Math.floor(Math.log10(max / 2)))
      let current = step
      while (current < max) {
        if (current > min) result.push(current)
        current += step
      }
    }
    
    // Sort the array
    return result.sort((a, b) => a - b)
  }
  
  // Generate nice key levels
  const keyLevels = generateNiceKeyLevels(min, max)
  
  // Generate tick marks with key points for labeling
  const tickCount = 20
  
  const ticks = Array.from({ length: tickCount + 1 }, (_, i) => {
    const tickPercentage = (i / tickCount) * 100
    const tickValue = min + (max - min) * (i / tickCount)
    
    // A tick is key if it's at 0%, 25%, 50%, 75%, or 100%
    const isKeyTick = 
      i === 0 || 
      i === tickCount || 
      Math.abs(tickPercentage - 25) < 0.01 || 
      Math.abs(tickPercentage - 50) < 0.01 || 
      Math.abs(tickPercentage - 75) < 0.01
    
    // Determine if this tick should have a label
    const shouldShowLabel = keyLevels.some(level => 
      Math.abs(tickValue - level) < 0.01 * (max - min)
    )
    
    return { 
      percentage: tickPercentage, 
      isKey: isKeyTick,
      showLabel: shouldShowLabel,
      value: tickValue
    }
  })

  const handleTrackClick = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!trackRef.current) return

    const rect = trackRef.current.getBoundingClientRect()
    const clickPosition = e.clientX - rect.left
    const rawPercentage = (clickPosition / rect.width) * 100
    
    // No rounding to steps of 5 for leverage slider - more precise control
    const newValue = min + (rawPercentage / 100) * (max - min)
    const clampedValue = Math.max(min, Math.min(max, Math.round(newValue)))
    
    setInternalValue(clampedValue)
    onChange?.(clampedValue)
  }

  const handleMouseDown = (e: React.MouseEvent<HTMLDivElement>) => {
    e.preventDefault()
    setIsDragging(true)
  }

  const handleMouseMove = (e: MouseEvent) => {
    if (!isDragging || !trackRef.current) return

    const rect = trackRef.current.getBoundingClientRect()
    const movePosition = e.clientX - rect.left
    const rawPercentage = (movePosition / rect.width) * 100
    
    // No rounding to steps of 5 for leverage slider - more precise control
    const newValue = min + (rawPercentage / 100) * (max - min)
    const clampedValue = Math.max(min, Math.min(max, Math.round(newValue)))
    
    setInternalValue(clampedValue)
    onChange?.(clampedValue)
  }

  const handleMouseUp = () => {
    setIsDragging(false)
  }

  // Add touch event handlers
  const handleTouchStart = (e: React.TouchEvent<HTMLDivElement>) => {
    e.preventDefault();
    setIsDragging(true);
    handleTouchMove(e);
  };

  const handleTouchMove = (e: React.TouchEvent<HTMLDivElement>) => {
    if (!isDragging || !trackRef.current) return;

    const rect = trackRef.current.getBoundingClientRect();
    const touch = e.touches[0];
    const movePosition = touch.clientX - rect.left;
    const rawPercentage = (movePosition / rect.width) * 100;
    
    const newValue = min + (rawPercentage / 100) * (max - min);
    const clampedValue = Math.max(min, Math.min(max, Math.round(newValue)));
    
    setInternalValue(clampedValue);
    onChange?.(clampedValue);
  };

  const handleTouchEnd = () => {
    setIsDragging(false);
  };

  useEffect(() => {
    if (isDragging) {
      window.addEventListener("mousemove", handleMouseMove);
      window.addEventListener("mouseup", handleMouseUp);
      window.addEventListener("touchmove", handleTouchMove as any, { passive: false });
      window.addEventListener("touchend", handleTouchEnd, { passive: false });
    }

    return () => {
      window.removeEventListener("mousemove", handleMouseMove);
      window.removeEventListener("mouseup", handleMouseUp);
      window.removeEventListener("touchmove", handleTouchMove as any);
      window.removeEventListener("touchend", handleTouchEnd);
    };
  }, [isDragging]);

  // Adjust the visual position for rendering elements
  const getAdjustedPosition = (percent: number) => {
    // Scale the percentage to fit within the padded area
    return edgePadding * 100 + percent * (1 - 2 * edgePadding) * 100
  }

  // Make the tick values clickable to set specific leverage values
  const handleTickValueClick = (value: number) => {
    setInternalValue(value);
    onChange?.(value);
  };

  return (
    <div className={cn("w-full max-w-md mx-auto pt-2 pb-10", className)}>
      {/* Tick Labels for key leverage values - positioned ABOVE the slider */}
      <div className="w-full mb-0 relative h-5">
        {keyLevels.map((keyValue, i) => {
          // Calculate percentage position for this key value
          const keyPercentage = ((keyValue - min) / (max - min)) * 100;
          
          return (
            <div
              key={i}
              className="absolute -translate-x-1/2 text-[10px] text-muted-foreground bg-background px-1.5 py-0.5 rounded bg-surface-1 hover:bg-background/80 cursor-pointer transition-colors"
              style={{
                left: `${getAdjustedPosition(keyPercentage / 100)}%`,
              }}
              onClick={() => handleTickValueClick(keyValue)}
            >
              {Math.round(keyValue)}x
            </div>
          );
        })}
      </div>

      <div
        ref={trackRef}
        className="relative h-2.5 bg-muted/50 rounded-full cursor-pointer mt-1"
        onClick={handleTrackClick}
        onMouseDown={handleMouseDown}
        onTouchStart={handleTouchStart}
        style={{ touchAction: "none" }}
      >
        {/* Ticks */}
        {ticks.map((tick, i) => {
          // Adjust visual position of ticks
          const adjustedPosition = getAdjustedPosition(tick.percentage / 100)
          
          return (
            <div
              key={i}
              className={cn("absolute -translate-x-1/2", tick.isKey ? "bg-white w-0.5 h-2" : "w-px h-1.5 bg-gray-600")}
              style={{ 
                left: `${adjustedPosition}%`, 
                top: "50%", 
                transform: "translate(-50%, -50%)" 
              }}
            />
          )
        })}

        {/* Progress Track with padding */}
        <div
          className="absolute top-1/2 h-1.5 -translate-y-1/2 rounded-full bg-white"
          style={{
            left: `${edgePadding * 100}%`,
            width: `${percentage * (1 - 2 * edgePadding)}%`,
            boxShadow: "0 0 8px 1px rgba(255, 255, 255, 0.7)",
          }}
        />

        {/* Value Label with X suffix */}
        <div
          className="absolute top-full mt-1 text-white text-xs font-medium"
          style={{
            left: `${getAdjustedPosition(percentage / 100)}%`,
            transform: "translateX(-50%)",
            textAlign: "center",
          }}
        >
          {Math.round(currentValue)}x
        </div>
      </div>
    </div>
  )
}

