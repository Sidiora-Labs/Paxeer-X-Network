import type { CapacitorConfig } from '@capacitor/cli';

const config: CapacitorConfig = {
  appId: 'com.paxeer.wallet',
  appName: 'Paxeer Wallet',
  webDir: 'out',
  server: {
    url: 'https://paxportwallet.com',
    cleartext: false,
  },
  android: {
    allowMixedContent: false,
    backgroundColor: '#050505',
    buildOptions: {
      signingType: 'apksigner',
    },
  },
  plugins: {
    SplashScreen: {
      launchAutoHide: true,
      launchShowDuration: 1500,
      backgroundColor: '#050505',
      showSpinner: false,
      androidScaleType: 'CENTER_CROP',
    },
    StatusBar: {
      style: 'DARK',
      backgroundColor: '#050505',
    },
    PushNotifications: {
      presentationOptions: ['badge', 'sound', 'alert'],
    },
    BiometricAuth: {
      allowDeviceCredential: true,
    },
  },
};

export default config;
