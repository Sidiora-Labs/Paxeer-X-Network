"use client"

import type React from "react"

import { useState, useRef, useEffect } from "react"
import { cn } from "@/ui/utils/cn"

interface CustomSliderProps {
  min?: number
  max?: number
  step?: number
  defaultValue?: number
  value?: number
  onChange?: (value: number) => void
  className?: string
}

export default function CustomSlider({
  min = 0,
  max = 100,
  step = 1,
  defaultValue = 0,
  value,
  onChange,
  className,
}: CustomSliderProps) {
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

  // Generate tick marks - increased to have marks at every 5% (21 ticks total)
  const tickCount = 20
  const ticks = Array.from({ length: tickCount + 1 }, (_, i) => {
    const tickPercentage = (i / tickCount) * 100
    // Highlight key ticks: 0%, 25%, 50%, 75%, and 100%
    const isKeyTick = 
      i === 0 || 
      i === tickCount || 
      Math.abs(tickPercentage - 25) < 0.01 || 
      Math.abs(tickPercentage - 50) < 0.01 || 
      Math.abs(tickPercentage - 75) < 0.01
    return { percentage: tickPercentage, isKey: isKeyTick }
  })

  // Round to nearest 5 for manual interaction
  const roundToNearest5 = (value: number) => {
    return Math.round(value / 5) * 5
  }

  const handleTrackClick = (e: React.MouseEvent<HTMLDivElement>) => {
    if (!trackRef.current) return

    const rect = trackRef.current.getBoundingClientRect()
    const clickPosition = e.clientX - rect.left
    const rawPercentage = (clickPosition / rect.width) * 100
    
    // Round to nearest 5%
    const roundedPercentage = roundToNearest5(rawPercentage)
    
    const newValue = Math.round((roundedPercentage / 100) * (max - min) + min)
    const clampedValue = Math.max(min, Math.min(max, newValue))
    
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
    
    // Round to nearest 5%
    const roundedPercentage = roundToNearest5(rawPercentage)
    
    const newValue = Math.round((roundedPercentage / 100) * (max - min) + min)
    const clampedValue = Math.max(min, Math.min(max, newValue))
    
    setInternalValue(clampedValue)
    onChange?.(clampedValue)
  }

  const handleMouseUp = () => {
    setIsDragging(false)
  }

  useEffect(() => {
    if (isDragging) {
      window.addEventListener("mousemove", handleMouseMove)
      window.addEventListener("mouseup", handleMouseUp)
    }

    return () => {
      window.removeEventListener("mousemove", handleMouseMove)
      window.removeEventListener("mouseup", handleMouseUp)
    }
  }, [isDragging])

  // Adjust the visual position for rendering elements
  const getAdjustedPosition = (percent: number) => {
    // Scale the percentage to fit within the padded area
    return edgePadding * 100 + percent * (1 - 2 * edgePadding) * 100
  }

  // Calculate progress bar width with padding consideration
  const getProgressWidth = () => {
    if (percentage === 0) return 0
    if (percentage === 100) return 100 - edgePadding * 200
    return percentage * (1 - 2 * edgePadding) * 100 / 100
  }

  return (
    <div className={cn("w-full max-w-md mx-auto mb-6", className)}>
      <div
        ref={trackRef}
        className="relative h-2.5 bg-muted/50 rounded-full cursor-pointer"
        onClick={handleTrackClick}
        onMouseDown={handleMouseDown}
      >
        {/* Ticks - Keep original positioning for user interaction */}
        {ticks.map((tick, i) => {
          // Adjust visual position of ticks
          const adjustedPosition = getAdjustedPosition(tick.percentage / 100)
          
          return (
            <div
              key={i}
              className={cn("absolute w-px h-1.5 -translate-x-1/2", tick.isKey ? "bg-white" : "bg-gray-600")}
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

        {/* Percentage Label - smaller font */}
        <div
          className="absolute top-full mt-1 text-white text-xs font-medium"
          style={{
            left: `${getAdjustedPosition(percentage / 100)}%`,
            transform: "translateX(-50%)",
            textAlign: "center",
          }}
        >
          {Math.round(percentage)}%
        </div>
      </div>
    </div>
  )
}

