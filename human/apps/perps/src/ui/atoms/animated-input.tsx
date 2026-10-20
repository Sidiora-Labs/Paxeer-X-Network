"use client";

import { AnimatePresence, motion } from "framer-motion";
import React, { useRef, useState } from "react";

import { cn } from "@/ui/utils/cn"

// skiper Animated Input

const AnimatedInput = ({
  inputValue,
  setInputValue,
  placeholder,
  className,
}: {
  inputValue: string;
  setInputValue: (value: string) => void;
  placeholder: string;
  className?: string;
}) => {
  const inputRef = useRef<HTMLInputElement>(null);

  // Format display value
  const displayValue = inputValue || "";
  const digits = displayValue.split("");

  return (
    <div className={cn("relative overflow-hidden text-center", className)}>
      <input
        ref={inputRef}
        type="text"
        className="inset-0 w-full cursor-pointer text-center text-[45px] font-semibold tracking-tight text-transparent caret-white outline-none"
        value={inputValue}
        onChange={(e) => setInputValue(e.target.value)}
      />
      <div className="pointer-events-none absolute inset-0 flex items-center justify-center">
        {inputValue === "" && (
          <span className="text-[45px] font-semibold tracking-tight opacity-20">
            {placeholder}
          </span>
        )}
        <AnimatePresence initial={false} mode="popLayout">
          {digits.map((digit, index) => (
            <motion.span
              key={`${digit}-${index}`}
              className="text-[45px] font-semibold tracking-tight"
              initial={{ y: "100%", opacity: 0 }}
              animate={{ y: "0%", opacity: 1 }}
              exit={{ y: "100%", opacity: 0 }}
              // transition={{
              //   delay: index * 0.02,
              // }}
            >
              {digit}
            </motion.span>
          ))}
        </AnimatePresence>
      </div>
    </div>
  );
};

const Skiper68 = () => {
  const [inputValue, setInputValue] = useState("");
  return (
    <div className="flex h-full w-full flex-col items-center justify-center">
      <div className="-mt-10 mb-20 grid content-start justify-items-center gap-6 text-center">
        <span className="after:to-foreground relative max-w-[12ch] text-xs uppercase leading-tight opacity-40 after:absolute after:left-1/2 after:top-full after:h-16 after:w-px after:bg-gradient-to-b after:from-transparent after:content-['']">
          Try Entering a number
        </span>
      </div>
      <AnimatedInput
        inputValue={inputValue}
        placeholder="000"
        setInputValue={setInputValue}
        className="text-center"
      />
    </div>
  );
};

export { Skiper68 };
