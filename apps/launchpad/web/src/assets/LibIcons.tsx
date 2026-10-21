"use client";

import React from "react";

export const MetaMask = ({ className }: { className?: string }) => (
  <svg className={className} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
    <path d="M21.3 2L13.2 8.1L14.7 4.5L21.3 2Z" fill="#E17726" stroke="#E17726" strokeWidth="0.25"/>
    <path d="M2.7 2L10.7 8.2L9.3 4.5L2.7 2Z" fill="#E27625" stroke="#E27625" strokeWidth="0.25"/>
    <path d="M18.5 16.6L16.3 20.1L20.8 21.3L22 16.7L18.5 16.6Z" fill="#E27625" stroke="#E27625" strokeWidth="0.25"/>
    <path d="M2 16.7L3.2 21.3L7.7 20.1L5.5 16.6L2 16.7Z" fill="#E27625" stroke="#E27625" strokeWidth="0.25"/>
    <path d="M7.5 10.5L6.3 12.3L10.8 12.5L10.6 7.7L7.5 10.5Z" fill="#E27625" stroke="#E27625" strokeWidth="0.25"/>
    <path d="M16.5 10.5L13.3 7.6L13.2 12.5L17.7 12.3L16.5 10.5Z" fill="#E27625" stroke="#E27625" strokeWidth="0.25"/>
    <path d="M7.7 20.1L10.5 18.7L8.1 16.7L7.7 20.1Z" fill="#E27625" stroke="#E27625" strokeWidth="0.25"/>
    <path d="M13.5 18.7L16.3 20.1L15.9 16.7L13.5 18.7Z" fill="#E27625" stroke="#E27625" strokeWidth="0.25"/>
  </svg>
);

export const CoinBase = ({ className }: { className?: string }) => (
  <svg className={className} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
    <rect width="24" height="24" rx="6" fill="#0052FF"/>
    <path d="M12 4C7.58 4 4 7.58 4 12C4 16.42 7.58 20 12 20C16.42 20 20 16.42 20 12C20 7.58 16.42 4 12 4ZM14.5 14.5H9.5V9.5H14.5V14.5Z" fill="white"/>
  </svg>
);

export const PhantomWallet = ({ className }: { className?: string }) => (
  <svg className={className || "size-6"} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
    <rect width="24" height="24" rx="6" fill="#AB9FF2"/>
    <circle cx="9" cy="11" r="1.5" fill="white"/>
    <circle cx="15" cy="11" r="1.5" fill="white"/>
    <path d="M5 12C5 8.13 8.13 5 12 5H16C18.21 5 20 6.79 20 9V12C20 15.87 16.87 19 13 19H12C8.13 19 5 15.87 5 12Z" stroke="white" strokeWidth="1.5"/>
  </svg>
);

export const TrustWallet = ({ className }: { className?: string }) => (
  <svg className={className || "size-6"} viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
    <rect width="24" height="24" rx="6" fill="#0500FF"/>
    <path d="M12 4L5 7.5V12C5 16.14 7.97 19.97 12 21C16.03 19.97 19 16.14 19 12V7.5L12 4Z" stroke="white" strokeWidth="1.5" fill="none"/>
  </svg>
);
