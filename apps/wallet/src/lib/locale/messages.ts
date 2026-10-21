import type { Locale } from './index';
import { hi, it, ja, ko } from './additional-messages';

export interface MessageCatalog {
  readonly common: {
    readonly ok: string;
    readonly cancel: string;
    readonly close: string;
    readonly back: string;
    readonly retry: string;
    readonly done: string;
    readonly confirm: string;
    readonly deny: string;
    readonly loading: string;
    readonly error: string;
    readonly success: string;
    readonly warning: string;
    readonly offline: string;
    readonly copy: string;
    readonly copied: string;
    readonly search: string;
    readonly noResults: string;
    readonly learnMore: string;
  };
  readonly nav: {
    readonly portfolio: string;
    readonly send: string;
    readonly receive: string;
    readonly swap: string;
    readonly discover: string;
    readonly settings: string;
    readonly dappBrowser: string;
    readonly contacts: string;
    readonly activity: string;
    readonly ramp: string;
  };
  readonly shell: {
    readonly walletLocked: string;
    readonly unlockWallet: string;
    readonly lockWallet: string;
    readonly switchAccount: string;
    readonly switchNetwork: string;
    readonly connectedDapps: string;
    readonly permissions: string;
    readonly revokePermission: string;
    readonly disconnectAll: string;
  };
  readonly settings: {
    readonly title: string;
    readonly general: string;
    readonly security: string;
    readonly advanced: string;
    readonly language: string;
    readonly currency: string;
    readonly notifications: string;
    readonly about: string;
    readonly version: string;
    readonly network: string;
    readonly customRpc: string;
    readonly developerMode: string;
    readonly showHexData: string;
    readonly backupWallet: string;
    readonly exportPrivateKey: string;
    readonly resetWallet: string;
    readonly manageContacts: string;
    readonly accounts: string;
    readonly addAccount: string;
    readonly deriveNext: string;
    readonly importPrivateKey: string;
    readonly importKeySubtitle: string;
    readonly lockWallet: string;
    readonly exportRecoveryPhrase: string;
    readonly hideRecoveryPhrase: string;
    readonly hidePrivateKey: string;
    readonly exportPkSubtitle: string;
    readonly viewOnExplorer: string;
    readonly addressBook: string;
    readonly contactsSubtitle: string;
    readonly displayPreferences: string;
    readonly networkRpc: string;
    readonly advancedSubtitle: string;
    readonly notificationsSubtitle: string;
    readonly connectedDappsSubtitle: string;
    readonly walletMode: string;
    readonly switchWalletMode: string;
    readonly selfCustodySubtitle: string;
    readonly managedSubtitle: string;
    readonly dangerZone: string;
    readonly eraseWallet: string;
    readonly eraseConfirm: string;
    readonly freshAuth: string;
    readonly freshAuthMnemonic: string;
    readonly freshAuthPk: string;
    readonly verificationFailed: string;
    readonly pkWarning: string;
    readonly copyPk: string;
    readonly copyPhrase: string;
    readonly signedInAs: string;
    readonly signOut: string;
    readonly signOutSubtitle: string;
    readonly switchToFunded: string;
    readonly switchToFundedSubtitle: string;
    readonly becomeFunded: string;
    readonly becomeFundedSubtitle: string;
    readonly switchToStandard: string;
    readonly switchToStandardSubtitle: string;
    readonly managedCustody: string;
    readonly customNonce: string;
    readonly customNoncePlaceholder: string;
    readonly customNonceWarning: string;
    readonly saveRpc: string;
    readonly saved: string;
    readonly rpcPlaceholder: string;
    readonly rpcHint: string;
    readonly displayInputData: string;
    readonly debugInformation: string;
    readonly noConnectedDapps: string;
    readonly noConnectedDappsHint: string;
    readonly lastVisited: string;
    readonly revoke: string;
    readonly revokeAll: string;
  };
  readonly receive: {
    readonly title: string;
    readonly yourAddress: string;
    readonly addressCopied: string;
    readonly share: string;
    readonly shareTitle: string;
  };
  readonly contacts: {
    readonly title: string;
    readonly addContact: string;
    readonly editContact: string;
    readonly newContact: string;
    readonly name: string;
    readonly namePlaceholder: string;
    readonly walletAddress: string;
    readonly note: string;
    readonly optional: string;
    readonly notePlaceholder: string;
    readonly saveContact: string;
    readonly updateContact: string;
    readonly deleteContact: string;
    readonly deleteConfirm: string;
    readonly noContacts: string;
    readonly noContactsHint: string;
    readonly searchPlaceholder: string;
    readonly noResults: string;
    readonly nameRequired: string;
    readonly addressRequired: string;
    readonly invalidAddress: string;
    readonly contactAdded: string;
    readonly contactUpdated: string;
  };
  readonly account: {
    readonly title: string;
    readonly switchAccount: string;
  };
  readonly tx: {
    readonly send: string;
    readonly confirm: string;
    readonly approving: string;
    readonly submitting: string;
    readonly submitted: string;
    readonly confirmed: string;
    readonly failed: string;
    readonly pending: string;
    readonly speedUp: string;
    readonly cancelTx: string;
    readonly nonce: string;
    readonly gasFee: string;
    readonly total: string;
    readonly to: string;
    readonly from: string;
    readonly amount: string;
    readonly balance: string;
    readonly max: string;
    readonly insufficientFunds: string;
    readonly invalidAddress: string;
  };
  readonly approval: {
    readonly requestFrom: string;
    readonly network: string;
    readonly operation: string;
    readonly risk: string;
    readonly simulation: string;
    readonly details: string;
    readonly rawData: string;
    readonly expirWarning: string;
    readonly highRisk: string;
    readonly unknownToken: string;
    readonly infiniteApproval: string;
  };
  readonly swap: {
    readonly title: string;
    readonly from: string;
    readonly to: string;
    readonly rate: string;
    readonly priceImpact: string;
    readonly slippage: string;
    readonly minimumReceived: string;
    readonly networkFee: string;
    readonly route: string;
    readonly quoteExpired: string;
    readonly fetchingQuote: string;
    readonly approveToken: string;
    readonly executeSwap: string;
    readonly noRoutes: string;
  };
  readonly a11y: {
    readonly openMenu: string;
    readonly closeMenu: string;
    readonly openDialog: string;
    readonly closeDialog: string;
    readonly nextPage: string;
    readonly previousPage: string;
    readonly showMore: string;
    readonly showLess: string;
    readonly flipTokens: string;
    readonly scanQr: string;
    readonly copyAddress: string;
    readonly goToPage: string;
    readonly currentPage: string;
  };
}

export const en: MessageCatalog = {
  common: {
    ok: 'OK',
    cancel: 'Cancel',
    close: 'Close',
    back: 'Back',
    retry: 'Retry',
    done: 'Done',
    confirm: 'Confirm',
    deny: 'Deny',
    loading: 'Loading\u2026',
    error: 'Something went wrong',
    success: 'Success',
    warning: 'Warning',
    offline: 'You\u2019re offline',
    copy: 'Copy',
    copied: 'Copied',
    search: 'Search',
    noResults: 'No results',
    learnMore: 'Learn more',
  },
  nav: {
    portfolio: 'Portfolio',
    send: 'Send',
    receive: 'Receive',
    swap: 'Swap',
    discover: 'Discover',
    settings: 'Settings',
    dappBrowser: 'dApps',
    contacts: 'Contacts',
    activity: 'Activity',
    ramp: 'Buy & Sell',
  },
  shell: {
    walletLocked: 'Wallet locked',
    unlockWallet: 'Unlock wallet',
    lockWallet: 'Lock wallet',
    switchAccount: 'Switch account',
    switchNetwork: 'Switch network',
    connectedDapps: 'Connected dApps',
    permissions: 'Permissions',
    revokePermission: 'Revoke permission',
    disconnectAll: 'Disconnect all',
  },
  settings: {
    title: 'Settings',
    general: 'General',
    security: 'Security',
    advanced: 'Advanced',
    language: 'Language',
    currency: 'Currency',
    notifications: 'Notifications',
    about: 'About',
    version: 'Version',
    network: 'Network',
    customRpc: 'Custom RPC',
    developerMode: 'Developer mode',
    showHexData: 'Show hex data',
    backupWallet: 'Backup wallet',
    exportPrivateKey: 'Export private key',
    resetWallet: 'Reset wallet',
    manageContacts: 'Manage contacts',
    accounts: 'Accounts',
    addAccount: 'Add Account',
    deriveNext: 'Derive next HD account',
    importPrivateKey: 'Import Private Key',
    importKeySubtitle: 'Add single account by key',
    lockWallet: 'Lock Wallet',
    exportRecoveryPhrase: 'Export Recovery Phrase',
    hideRecoveryPhrase: 'Hide Recovery Phrase',
    hidePrivateKey: 'Hide Private Key',
    exportPkSubtitle: 'Export active account private key',
    viewOnExplorer: 'View on Explorer',
    addressBook: 'Address Book',
    contactsSubtitle: 'Manage saved wallet addresses',
    displayPreferences: 'Display Preferences',
    networkRpc: 'Network & RPC',
    advancedSubtitle: 'Nonce, gas, developer tools',
    notificationsSubtitle: 'Price alerts, tx updates',
    connectedDappsSubtitle: 'Manage dApp sessions',
    walletMode: 'Wallet Mode',
    switchWalletMode: 'Switch wallet mode',
    selfCustodySubtitle: 'Move to a self-custody wallet',
    managedSubtitle: 'Switch to managed wallet',
    dangerZone: 'Danger Zone',
    eraseWallet: 'Erase Wallet',
    eraseConfirm: 'Tap again to erase wallet',
    freshAuth: 'Fresh Authentication',
    freshAuthMnemonic: 'Enter your PIN to export the recovery phrase.',
    freshAuthPk: 'Enter your PIN to export the private key.',
    verificationFailed: 'Verification failed',
    pkWarning: 'Never share your private key. Anyone with it can steal your funds.',
    copyPk: 'Copy private key',
    copyPhrase: 'Copy phrase',
    signedInAs: 'Signed in as',
    signOut: 'Sign out',
    signOutSubtitle: 'End your wallet session on this device',
    switchToFunded: 'Switch to Funded Account',
    switchToFundedSubtitle: 'View your funded equity, drawdown, and tier',
    becomeFunded: 'Become a Funded Trader',
    becomeFundedSubtitle: 'Trade with funded capital. No deposit, profit share applies.',
    switchToStandard: 'Switch to Standard Wallet',
    switchToStandardSubtitle: 'View your standard managed wallet',
    managedCustody: 'managed custody',
    customNonce: 'Custom Nonce',
    customNoncePlaceholder: 'Leave blank for auto',
    customNonceWarning: 'Only set this if you understand nonce ordering. An incorrect nonce can cause a transaction to fail.',
    saveRpc: 'Save RPC',
    saved: 'Saved',
    rpcPlaceholder: 'https://rpc.example.com',
    rpcHint: 'Leave blank to use the default RPC node',
    displayInputData: 'Display input data in transactions',
    debugInformation: 'Show raw data and debug information',
    noConnectedDapps: 'No connected dApps',
    noConnectedDappsHint: 'dApps you visit in the browser will appear here',
    lastVisited: 'Last visited',
    revoke: 'Revoke',
    revokeAll: 'Revoke All Sessions',
  },
  tx: {
    send: 'Send',
    confirm: 'Confirm transaction',
    approving: 'Approving\u2026',
    submitting: 'Submitting\u2026',
    submitted: 'Submitted',
    confirmed: 'Confirmed',
    failed: 'Failed',
    pending: 'Pending',
    speedUp: 'Speed up',
    cancelTx: 'Cancel transaction',
    nonce: 'Nonce',
    gasFee: 'Network fee',
    total: 'Total',
    to: 'To',
    from: 'From',
    amount: 'Amount',
    balance: 'Balance',
    max: 'Max',
    insufficientFunds: 'Insufficient funds',
    invalidAddress: 'Invalid address',
  },
  approval: {
    requestFrom: 'Request from',
    network: 'Network',
    operation: 'Operation',
    risk: 'Risk',
    simulation: 'Simulation',
    details: 'Details',
    rawData: 'Raw data',
    expirWarning: 'This request expires soon',
    highRisk: 'High risk',
    unknownToken: 'Unknown token',
    infiniteApproval: 'Infinite approval',
  },
  swap: {
    title: 'Swap',
    from: 'You pay',
    to: 'You receive',
    rate: 'Rate',
    priceImpact: 'Price impact',
    slippage: 'Slippage tolerance',
    minimumReceived: 'Minimum received',
    networkFee: 'Network fee',
    route: 'Route',
    quoteExpired: 'Quote expired',
    fetchingQuote: 'Fetching quote\u2026',
    approveToken: 'Approve token',
    executeSwap: 'Execute swap',
    noRoutes: 'No routes available',
  },
  a11y: {
    openMenu: 'Open menu',
    closeMenu: 'Close menu',
    openDialog: 'Open dialog',
    closeDialog: 'Close dialog',
    nextPage: 'Next page',
    previousPage: 'Previous page',
    showMore: 'Show more',
    showLess: 'Show less',
    flipTokens: 'Flip tokens',
    scanQr: 'Scan QR code',
    copyAddress: 'Copy address',
    goToPage: 'Go to page',
    currentPage: 'Current page',
  },
  receive: {
    title: 'Receive',
    yourAddress: 'Your address',
    addressCopied: 'Address copied',
    share: 'Share',
    shareTitle: 'Wallet Address',
  },
  contacts: {
    title: 'Contacts',
    addContact: 'Add Contact',
    editContact: 'Edit Contact',
    newContact: 'New Contact',
    name: 'Name',
    namePlaceholder: 'e.g. Alice',
    walletAddress: 'Wallet Address',
    note: 'Note',
    optional: 'optional',
    notePlaceholder: 'e.g. Team multisig',
    saveContact: 'Save Contact',
    updateContact: 'Update Contact',
    deleteContact: 'Delete Contact',
    deleteConfirm: 'This will permanently remove this contact from your address book.',
    noContacts: 'No contacts yet',
    noContactsHint: 'Save wallet addresses for quick sending',
    searchPlaceholder: 'Search contacts\u2026',
    noResults: 'No results for',
    nameRequired: 'Name is required',
    addressRequired: 'Address is required',
    invalidAddress: 'Invalid Ethereum address',
    contactAdded: 'added',
    contactUpdated: 'updated',
  },
  account: {
    title: 'Account',
    switchAccount: 'Switch Account',
  },
} as const;

const es: MessageCatalog = {
  common: {
    ok: 'Aceptar',
    cancel: 'Cancelar',
    close: 'Cerrar',
    back: 'Atrás',
    retry: 'Reintentar',
    done: 'Listo',
    confirm: 'Confirmar',
    deny: 'Rechazar',
    loading: 'Cargando\u2026',
    error: 'Algo salió mal',
    success: 'Éxito',
    warning: 'Advertencia',
    offline: 'Sin conexión',
    copy: 'Copiar',
    copied: 'Copiado',
    search: 'Buscar',
    noResults: 'Sin resultados',
    learnMore: 'Más información',
  },
  nav: {
    portfolio: 'Portafolio',
    send: 'Enviar',
    receive: 'Recibir',
    swap: 'Intercambio',
    discover: 'Descubrir',
    settings: 'Ajustes',
    dappBrowser: 'dApps',
    contacts: 'Contactos',
    activity: 'Actividad',
    ramp: 'Comprar y vender',
  },
  shell: {
    walletLocked: 'Cartera bloqueada',
    unlockWallet: 'Desbloquear cartera',
    lockWallet: 'Bloquear cartera',
    switchAccount: 'Cambiar cuenta',
    switchNetwork: 'Cambiar red',
    connectedDapps: 'dApps conectadas',
    permissions: 'Permisos',
    revokePermission: 'Revocar permiso',
    disconnectAll: 'Desconectar todo',
  },
  settings: {
    title: 'Ajustes',
    general: 'General',
    security: 'Seguridad',
    advanced: 'Avanzado',
    language: 'Idioma',
    currency: 'Moneda',
    notifications: 'Notificaciones',
    about: 'Acerca de',
    version: 'Versión',
    network: 'Red',
    customRpc: 'RPC personalizado',
    developerMode: 'Modo desarrollador',
    showHexData: 'Mostrar datos hex',
    backupWallet: 'Respaldar cartera',
    exportPrivateKey: 'Exportar clave privada',
    resetWallet: 'Restablecer cartera',
    manageContacts: 'Gestionar contactos',
    accounts: 'Cuentas',
    addAccount: 'Agregar cuenta',
    deriveNext: 'Derivar siguiente cuenta HD',
    importPrivateKey: 'Importar clave privada',
    importKeySubtitle: 'Agregar cuenta individual por clave',
    lockWallet: 'Bloquear cartera',
    exportRecoveryPhrase: 'Exportar frase de recuperación',
    hideRecoveryPhrase: 'Ocultar frase de recuperación',
    hidePrivateKey: 'Ocultar clave privada',
    exportPkSubtitle: 'Exportar clave privada de la cuenta activa',
    viewOnExplorer: 'Ver en explorador',
    addressBook: 'Libreta de direcciones',
    contactsSubtitle: 'Gestionar direcciones guardadas',
    displayPreferences: 'Preferencias de pantalla',
    networkRpc: 'Red y RPC',
    advancedSubtitle: 'Nonce, gas, herramientas de desarrollo',
    notificationsSubtitle: 'Alertas de precio, actualizaciones de tx',
    connectedDappsSubtitle: 'Gestionar sesiones de dApps',
    walletMode: 'Modo de cartera',
    switchWalletMode: 'Cambiar modo de cartera',
    selfCustodySubtitle: 'Cambiar a cartera de autocustodia',
    managedSubtitle: 'Cambiar a cartera gestionada',
    dangerZone: 'Zona de peligro',
    eraseWallet: 'Borrar cartera',
    eraseConfirm: 'Toca de nuevo para borrar la cartera',
    freshAuth: 'Autenticación requerida',
    freshAuthMnemonic: 'Ingresa tu PIN para exportar la frase de recuperación.',
    freshAuthPk: 'Ingresa tu PIN para exportar la clave privada.',
    verificationFailed: 'Verificación fallida',
    pkWarning: 'Nunca compartas tu clave privada. Cualquiera con ella puede robar tus fondos.',
    copyPk: 'Copiar clave privada',
    copyPhrase: 'Copiar frase',
    signedInAs: 'Sesión iniciada como',
    signOut: 'Cerrar sesión',
    signOutSubtitle: 'Finalizar tu sesión en este dispositivo',
    switchToFunded: 'Cambiar a cuenta fondeada',
    switchToFundedSubtitle: 'Ver tu capital, drawdown y nivel',
    becomeFunded: 'Convertirse en trader fondeado',
    becomeFundedSubtitle: 'Opera con capital fondeado. Sin depósito, se aplica reparto de ganancias.',
    switchToStandard: 'Cambiar a cartera estándar',
    switchToStandardSubtitle: 'Ver tu cartera gestionada estándar',
    managedCustody: 'custodia gestionada',
    customNonce: 'Nonce personalizado',
    customNoncePlaceholder: 'Dejar en blanco para automático',
    customNonceWarning: 'Solo establece esto si entiendes el orden de nonces. Un nonce incorrecto puede causar el fallo de una transacción.',
    saveRpc: 'Guardar RPC',
    saved: 'Guardado',
    rpcPlaceholder: 'https://rpc.ejemplo.com',
    rpcHint: 'Dejar en blanco para usar el nodo RPC predeterminado',
    displayInputData: 'Mostrar datos de entrada en transacciones',
    debugInformation: 'Mostrar datos sin depurar e información de depuración',
    noConnectedDapps: 'No hay dApps conectadas',
    noConnectedDappsHint: 'Las dApps que visites en el navegador aparecerán aquí',
    lastVisited: 'Última visita',
    revoke: 'Revocar',
    revokeAll: 'Revocar todas las sesiones',
  },
  tx: {
    send: 'Enviar',
    confirm: 'Confirmar transacción',
    approving: 'Aprobando\u2026',
    submitting: 'Enviando\u2026',
    submitted: 'Enviada',
    confirmed: 'Confirmada',
    failed: 'Fallida',
    pending: 'Pendiente',
    speedUp: 'Acelerar',
    cancelTx: 'Cancelar transacción',
    nonce: 'Nonce',
    gasFee: 'Comisión de red',
    total: 'Total',
    to: 'Para',
    from: 'De',
    amount: 'Cantidad',
    balance: 'Saldo',
    max: 'Máx',
    insufficientFunds: 'Fondos insuficientes',
    invalidAddress: 'Dirección inválida',
  },
  approval: {
    requestFrom: 'Solicitud de',
    network: 'Red',
    operation: 'Operación',
    risk: 'Riesgo',
    simulation: 'Simulación',
    details: 'Detalles',
    rawData: 'Datos sin procesar',
    expirWarning: 'Esta solicitud expira pronto',
    highRisk: 'Alto riesgo',
    unknownToken: 'Token desconocido',
    infiniteApproval: 'Aprobación infinita',
  },
  swap: {
    title: 'Intercambio',
    from: 'Pagas',
    to: 'Recibes',
    rate: 'Tasa',
    priceImpact: 'Impacto en precio',
    slippage: 'Tolerancia de deslizamiento',
    minimumReceived: 'Mínimo recibido',
    networkFee: 'Comisión de red',
    route: 'Ruta',
    quoteExpired: 'Cotización expirada',
    fetchingQuote: 'Obteniendo cotización\u2026',
    approveToken: 'Aprobar token',
    executeSwap: 'Ejecutar intercambio',
    noRoutes: 'No hay rutas disponibles',
  },
  a11y: {
    openMenu: 'Abrir menú',
    closeMenu: 'Cerrar menú',
    openDialog: 'Abrir diálogo',
    closeDialog: 'Cerrar diálogo',
    nextPage: 'Página siguiente',
    previousPage: 'Página anterior',
    showMore: 'Mostrar más',
    showLess: 'Mostrar menos',
    flipTokens: 'Invertir tokens',
    scanQr: 'Escanear código QR',
    copyAddress: 'Copiar dirección',
    goToPage: 'Ir a la página',
    currentPage: 'Página actual',
  },
  receive: {
    title: 'Recibir',
    yourAddress: 'Tu dirección',
    addressCopied: 'Dirección copiada',
    share: 'Compartir',
    shareTitle: 'Dirección de billetera',
  },
  contacts: {
    title: 'Contactos',
    addContact: 'Agregar contacto',
    editContact: 'Editar contacto',
    newContact: 'Nuevo contacto',
    name: 'Nombre',
    namePlaceholder: 'ej. Alice',
    walletAddress: 'Dirección de billetera',
    note: 'Nota',
    optional: 'opcional',
    notePlaceholder: 'ej. Multisig del equipo',
    saveContact: 'Guardar contacto',
    updateContact: 'Actualizar contacto',
    deleteContact: 'Eliminar contacto',
    deleteConfirm: 'Esto eliminará permanentemente este contacto de tu libreta de direcciones.',
    noContacts: 'Aún no hay contactos',
    noContactsHint: 'Guarda direcciones de billetera para envíos rápidos',
    searchPlaceholder: 'Buscar contactos\u2026',
    noResults: 'Sin resultados para',
    nameRequired: 'El nombre es obligatorio',
    addressRequired: 'La dirección es obligatoria',
    invalidAddress: 'Dirección Ethereum no válida',
    contactAdded: 'agregado',
    contactUpdated: 'actualizado',
  },
  account: {
    title: 'Cuenta',
    switchAccount: 'Cambiar cuenta',
  },
} as const;
const ar: MessageCatalog = {
  common: {
    ok: 'موافق',
    cancel: 'إلغاء',
    close: 'إغلاق',
    back: 'رجوع',
    retry: 'إعادة المحاولة',
    done: 'تم',
    confirm: 'تأكيد',
    deny: 'رفض',
    loading: 'جارِ التحميل\u2026',
    error: 'حدث خطأ ما',
    success: 'نجاح',
    warning: 'تحذير',
    offline: 'أنت غير متصل',
    copy: 'نسخ',
    copied: 'تم النسخ',
    search: 'بحث',
    noResults: 'لا نتائج',
    learnMore: 'اعرف المزيد',
  },
  nav: {
    portfolio: 'المحفظة',
    send: 'إرسال',
    receive: 'استقبال',
    swap: 'تبديل',
    discover: 'استكشاف',
    settings: 'الإعدادات',
    dappBrowser: 'التطبيقات',
    contacts: 'جهات الاتصال',
    activity: 'النشاط',
    ramp: 'شراء وبيع',
  },
  shell: {
    walletLocked: 'المحفظة مقفلة',
    unlockWallet: 'فتح المحفظة',
    lockWallet: 'قفل المحفظة',
    switchAccount: 'تبديل الحساب',
    switchNetwork: 'تبديل الشبكة',
    connectedDapps: 'التطبيقات المتصلة',
    permissions: 'الأذونات',
    revokePermission: 'إذن الإلغاء',
    disconnectAll: 'فصل الكل',
  },
  settings: {
    title: 'الإعدادات',
    general: 'عام',
    security: 'الأمان',
    advanced: 'متقدم',
    language: 'اللغة',
    currency: 'العملة',
    notifications: 'الإشعارات',
    about: 'حول',
    version: 'الإصدار',
    network: 'الشبكة',
    customRpc: 'RPC مخصص',
    developerMode: 'وضع المطور',
    showHexData: 'إظهار البيانات السداسية',
    backupWallet: 'نسخ احتياطي للمحفظة',
    exportPrivateKey: 'تصدير المفتاح الخاص',
    resetWallet: 'إعادة تعيين المحفظة',
    manageContacts: 'إدارة جهات الاتصال',
    accounts: 'الحسابات',
    addAccount: 'إضافة حساب',
    deriveNext: 'اشتقاق الحساب HD التالي',
    importPrivateKey: 'استيراد المفتاح الخاص',
    importKeySubtitle: 'إضافة حساب واحد بالمفتاح',
    lockWallet: 'قفل المحفظة',
    exportRecoveryPhrase: 'تصدير عبارة الاسترداد',
    hideRecoveryPhrase: 'إخفاء عبارة الاسترداد',
    hidePrivateKey: 'إخفاء المفتاح الخاص',
    exportPkSubtitle: 'تصدير المفتاح الخاص للحساب النشط',
    viewOnExplorer: 'المشاهدة في المستكشف',
    addressBook: 'دفتر العناوين',
    contactsSubtitle: 'إدارة عناوين المحافظ المحفوظة',
    displayPreferences: 'تفضيلات العرض',
    networkRpc: 'الشبكة و RPC',
    advancedSubtitle: 'العدد المتسلسل، الغاز، أدوات المطور',
    notificationsSubtitle: 'تنبيهات الأسعار، تحديثات المعاملات',
    connectedDappsSubtitle: 'إدارة جلسات التطبيقات',
    walletMode: 'وضع المحفظة',
    switchWalletMode: 'تبديل وضع المحفظة',
    selfCustodySubtitle: 'الانتقال إلى محفظة ذاتية الحضانة',
    managedSubtitle: 'الانتقال إلى محفظة مُدارة',
    dangerZone: 'منطقة الخطر',
    eraseWallet: 'مسح المحفظة',
    eraseConfirm: 'اضغط مرة أخرى لمسح المحفظة',
    freshAuth: 'مطلوب مصادقة جديدة',
    freshAuthMnemonic: 'أدخل رقم PIN لتصدير عبارة الاسترداد.',
    freshAuthPk: 'أدخل رقم PIN لتصدير المفتاح الخاص.',
    verificationFailed: 'فشل التحقق',
    pkWarning: 'لا تشارك مفتاحك الخاص أبدًا. أي شخص لديه يمكنه سرقة أموالك.',
    copyPk: 'نسخ المفتاح الخاص',
    copyPhrase: 'نسخ العبارة',
    signedInAs: 'مسجل الدخول كـ',
    signOut: 'تسجيل الخروج',
    signOutSubtitle: 'إنهاء جلستك على هذا الجهاز',
    switchToFunded: 'التبديل إلى حساب ممول',
    switchToFundedSubtitle: 'عرض رأس المال والانخفاض والمستوى',
    becomeFunded: 'הפוך לסוחר ממומן',
    becomeFundedSubtitle: 'تداول برأس مال ممول. بدون إيداع، يُطبق تقاسم الأرباح.',
    switchToStandard: 'التبديل إلى المحفظة القياسية',
    switchToStandardSubtitle: 'عرض محفظتك المُدارة القياسية',
    managedCustody: 'حضانة مُدارة',
    customNonce: 'العدد المتسلسل مخصص',
    customNoncePlaceholder: 'اتركه فارغًا للتلقائي',
    customNonceWarning: 'قم بتعيين هذا فقط إذا كنت تفهم ترتيب العدد المتسلسل. عدد متسلسل خاطئ قد يتسبب في فشل المعاملة.',
    saveRpc: 'حفظ RPC',
    saved: 'تم الحفظ',
    rpcPlaceholder: 'https://rpc.example.com',
    rpcHint: 'اتركه فارغًا لاستخدام عقدة RPC الافتراضية',
    displayInputData: 'إظهار بيانات الإدخال في المعاملات',
    debugInformation: 'إظهار البيانات الخام وتصحيح الأخطاء',
    noConnectedDapps: 'لا توجد تطبيقات متصلة',
    noConnectedDappsHint: 'التطبيقات التي تزورها في المتصفح ستظهر هنا',
    lastVisited: 'آخر زيارة',
    revoke: 'إلغاء',
    revokeAll: 'إلغاء جميع الجلسات',
  },
  tx: {
    send: 'إرسال',
    confirm: 'تأكيد المعاملة',
    approving: 'جارِ الموافقة\u2026',
    submitting: 'جارِ الإرسال\u2026',
    submitted: 'تم الإرسال',
    confirmed: 'مؤكدة',
    failed: 'فشلت',
    pending: 'قيد الانتظار',
    speedUp: 'تسريع',
    cancelTx: 'إلغاء المعاملة',
    nonce: 'العدد المتسلسل',
    gasFee: 'رسوم الشبكة',
    total: 'الإجمالي',
    to: 'إلى',
    from: 'من',
    amount: 'المبلغ',
    balance: 'الرصيد',
    max: 'الحد الأقصى',
    insufficientFunds: 'أموال غير كافية',
    invalidAddress: 'عنوان غير صالح',
  },
  approval: {
    requestFrom: 'طلب من',
    network: 'الشبكة',
    operation: 'العملية',
    risk: 'المخاطرة',
    simulation: 'المحاكاة',
    details: 'التفاصيل',
    rawData: 'البيانات الخام',
    expirWarning: 'هذا الطلب سينتهي قريبًا',
    highRisk: 'مخاطرة عالية',
    unknownToken: 'رمز غير معروف',
    infiniteApproval: 'موافقة غير محدودة',
  },
  swap: {
    title: 'تبديل',
    from: 'تدفع',
    to: 'تستقبل',
    rate: 'السعر',
    priceImpact: 'التأثير على السعر',
    slippage: 'تحمّل الانزلاق',
    minimumReceived: 'الحد الأدنى المستلم',
    networkFee: 'رسوم الشبكة',
    route: 'المسار',
    quoteExpired: 'انتهت صلاحية عرض السعر',
    fetchingQuote: 'جارِ جلب عرض السعر\u2026',
    approveToken: 'الموافقة على الرمز',
    executeSwap: 'تنفيذ التبديل',
    noRoutes: 'لا توجد مسارات متاحة',
  },
  a11y: {
    openMenu: 'فتح القائمة',
    closeMenu: 'إغلاق القائمة',
    openDialog: 'فتح مربع الحوار',
    closeDialog: 'إغلاق مربع الحوار',
    nextPage: 'الصفحة التالية',
    previousPage: 'الصفحة السابقة',
    showMore: 'عرض المزيد',
    showLess: 'عرض أقل',
    flipTokens: 'تبديل الرموز',
    scanQr: 'مسح رمز QR',
    copyAddress: 'نسخ العنوان',
    goToPage: 'الذهاب إلى الصفحة',
    currentPage: 'الصفحة الحالية',
  },
  receive: {
    title: 'استقبال',
    yourAddress: 'عنوانك',
    addressCopied: 'تم نسخ العنوان',
    share: 'مشاركة',
    shareTitle: 'عنوان المحفظة',
  },
  contacts: {
    title: 'جهات الاتصال',
    addContact: 'إضافة جهة اتصال',
    editContact: 'تعديل جهة الاتصال',
    newContact: 'جهة اتصال جديدة',
    name: 'الاسم',
    namePlaceholder: 'مثال: أحمد',
    walletAddress: 'عنوان المحفظة',
    note: 'ملاحظة',
    optional: 'اختياري',
    notePlaceholder: 'مثال: multisig الفريق',
    saveContact: 'حفظ جهة الاتصال',
    updateContact: 'تحديث جهة الاتصال',
    deleteContact: 'حذف جهة الاتصال',
    deleteConfirm: 'سيؤدي هذا إلى إزالة جهة الاتصال هذه نهائيًا من دفتر العناوين.',
    noContacts: 'لا توجد جهات اتصال بعد',
    noContactsHint: 'احفظ عناوين المحافظ للإرسال السريع',
    searchPlaceholder: 'البحث في جهات الاتصال\u2026',
    noResults: 'لا نتائج لـ',
    nameRequired: 'الاسم مطلوب',
    addressRequired: 'العنوان مطلوب',
    invalidAddress: 'عنوان إيثريوم غير صالح',
    contactAdded: 'تمت الإضافة',
    contactUpdated: 'تم التحديث',
  },
  account: {
    title: 'الحساب',
    switchAccount: 'تبديل الحساب',
  },
} as const;

const fr: MessageCatalog = {
  common: {
    ok: 'OK',
    cancel: 'Annuler',
    close: 'Fermer',
    back: 'Retour',
    retry: 'Réessayer',
    done: 'Terminé',
    confirm: 'Confirmer',
    deny: 'Refuser',
    loading: 'Chargement\u2026',
    error: 'Une erreur est survenue',
    success: 'Succès',
    warning: 'Attention',
    offline: 'Vous êtes hors ligne',
    copy: 'Copier',
    copied: 'Copié',
    search: 'Rechercher',
    noResults: 'Aucun résultat',
    learnMore: 'En savoir plus',
  },
  nav: {
    portfolio: 'Portefeuille',
    send: 'Envoyer',
    receive: 'Recevoir',
    swap: 'Échanger',
    discover: 'Découvrir',
    settings: 'Paramètres',
    dappBrowser: 'dApps',
    contacts: 'Contacts',
    activity: 'Activité',
    ramp: 'Acheter & Vendre',
  },
  shell: {
    walletLocked: 'Portefeuille verrouillé',
    unlockWallet: 'Déverrouiller le portefeuille',
    lockWallet: 'Verrouiller le portefeuille',
    switchAccount: 'Changer de compte',
    switchNetwork: 'Changer de réseau',
    connectedDapps: 'dApps connectées',
    permissions: 'Permissions',
    revokePermission: 'Révoquer la permission',
    disconnectAll: 'Tout déconnecter',
  },
  settings: {
    title: 'Paramètres',
    general: 'Général',
    security: 'Sécurité',
    advanced: 'Avancé',
    language: 'Langue',
    currency: 'Devise',
    notifications: 'Notifications',
    about: 'À propos',
    version: 'Version',
    network: 'Réseau',
    customRpc: 'RPC personnalisé',
    developerMode: 'Mode développeur',
    showHexData: 'Afficher les données hexadécimales',
    backupWallet: 'Sauvegarder le portefeuille',
    exportPrivateKey: 'Exporter la clé privée',
    resetWallet: 'Réinitialiser le portefeuille',
    manageContacts: 'Gérer les contacts',
    accounts: 'Comptes',
    addAccount: 'Ajouter un compte',
    deriveNext: 'Dériver le prochain compte HD',
    importPrivateKey: 'Importer une clé privée',
    importKeySubtitle: 'Ajouter un compte unique par clé',
    lockWallet: 'Verrouiller le portefeuille',
    exportRecoveryPhrase: 'Exporter la phrase de récupération',
    hideRecoveryPhrase: 'Masquer la phrase de récupération',
    hidePrivateKey: 'Masquer la clé privée',
    exportPkSubtitle: 'Exporter la clé privée du compte actif',
    viewOnExplorer: 'Voir sur l\'explorateur',
    addressBook: 'Carnet d\'adresses',
    contactsSubtitle: 'Gérer les adresses enregistrées',
    displayPreferences: 'Préférences d\'affichage',
    networkRpc: 'Réseau et RPC',
    advancedSubtitle: 'Nonce, gas, outils développeur',
    notificationsSubtitle: 'Alertes de prix, mises à jour des tx',
    connectedDappsSubtitle: 'Gérer les sessions dApps',
    walletMode: 'Mode portefeuille',
    switchWalletMode: 'Changer de mode',
    selfCustodySubtitle: 'Passer en auto-garde',
    managedSubtitle: 'Passer en portefeuille géré',
    dangerZone: 'Zone de danger',
    eraseWallet: 'Effacer le portefeuille',
    eraseConfirm: 'Appuyez à nouveau pour effacer',
    freshAuth: 'Authentification requise',
    freshAuthMnemonic: 'Entrez votre code PIN pour exporter la phrase de récupération.',
    freshAuthPk: 'Entrez votre code PIN pour exporter la clé privée.',
    verificationFailed: 'Échec de la vérification',
    pkWarning: 'Ne partagez jamais votre clé privée. Quiconque la possède peut voler vos fonds.',
    copyPk: 'Copier la clé privée',
    copyPhrase: 'Copier la phrase',
    signedInAs: 'Connecté en tant que',
    signOut: 'Se déconnecter',
    signOutSubtitle: 'Terminer votre session sur cet appareil',
    switchToFunded: 'Passer au compte financé',
    switchToFundedSubtitle: 'Voir votre capital, drawdown et niveau',
    becomeFunded: 'Devenir un trader financé',
    becomeFundedSubtitle: 'Tradez avec du capital financé. Pas de dépôt, partage des bénéfices.',
    switchToStandard: 'Passer au portefeuille standard',
    switchToStandardSubtitle: 'Voir votre portefeuille géré standard',
    managedCustody: 'garde gérée',
    customNonce: 'Nonce personnalisé',
    customNoncePlaceholder: 'Laisser vide pour auto',
    customNonceWarning: 'Ne définissez cela que si vous comprenez l\'ordre des nonces. Un nonce incorrect peut entraîner l\'échec d\'une transaction.',
    saveRpc: 'Enregistrer RPC',
    saved: 'Enregistré',
    rpcPlaceholder: 'https://rpc.exemple.com',
    rpcHint: 'Laisser vide pour utiliser le nœud RPC par défaut',
    displayInputData: 'Afficher les données d\'entrée dans les transactions',
    debugInformation: 'Afficher les données brutes et le débogage',
    noConnectedDapps: 'Aucune dApp connectée',
    noConnectedDappsHint: 'Les dApps que vous visitez dans le navigateur apparaîtront ici',
    lastVisited: 'Dernière visite',
    revoke: 'Révoquer',
    revokeAll: 'Révoquer toutes les sessions',
  },
  tx: {
    send: 'Envoyer',
    confirm: 'Confirmer la transaction',
    approving: 'Approbation\u2026',
    submitting: 'Soumission\u2026',
    submitted: 'Soumise',
    confirmed: 'Confirmée',
    failed: 'Échouée',
    pending: 'En attente',
    speedUp: 'Accélérer',
    cancelTx: 'Annuler la transaction',
    nonce: 'Nonce',
    gasFee: 'Frais de réseau',
    total: 'Total',
    to: 'À',
    from: 'De',
    amount: 'Montant',
    balance: 'Solde',
    max: 'Max',
    insufficientFunds: 'Fonds insuffisants',
    invalidAddress: 'Adresse invalide',
  },
  approval: {
    requestFrom: 'Demande de',
    network: 'Réseau',
    operation: 'Opération',
    risk: 'Risque',
    simulation: 'Simulation',
    details: 'Détails',
    rawData: 'Données brutes',
    expirWarning: 'Cette demande expire bientôt',
    highRisk: 'Risque élevé',
    unknownToken: 'Jeton inconnu',
    infiniteApproval: 'Approbation infinie',
  },
  swap: {
    title: 'Échange',
    from: 'Vous payez',
    to: 'Vous recevez',
    rate: 'Taux',
    priceImpact: 'Impact sur le prix',
    slippage: 'Tolérance de glissement',
    minimumReceived: 'Minimum reçu',
    networkFee: 'Frais de réseau',
    route: 'Itinéraire',
    quoteExpired: 'Citation expirée',
    fetchingQuote: 'Récupération de la citation\u2026',
    approveToken: 'Approuver le jeton',
    executeSwap: 'Exécuter l\'échange',
    noRoutes: 'Aucun itinéraire disponible',
  },
  a11y: {
    openMenu: 'Ouvrir le menu',
    closeMenu: 'Fermer le menu',
    openDialog: 'Ouvrir la boîte de dialogue',
    closeDialog: 'Fermer la boîte de dialogue',
    nextPage: 'Page suivante',
    previousPage: 'Page précédente',
    showMore: 'Afficher plus',
    showLess: 'Afficher moins',
    flipTokens: 'Inverser les jetons',
    scanQr: 'Scanner le code QR',
    copyAddress: 'Copier l\'adresse',
    goToPage: 'Aller à la page',
    currentPage: 'Page actuelle',
  },
  receive: {
    title: 'Recevoir',
    yourAddress: 'Votre adresse',
    addressCopied: 'Adresse copiée',
    share: 'Partager',
    shareTitle: 'Adresse du portefeuille',
  },
  contacts: {
    title: 'Contacts',
    addContact: 'Ajouter un contact',
    editContact: 'Modifier le contact',
    newContact: 'Nouveau contact',
    name: 'Nom',
    namePlaceholder: 'ex. Alice',
    walletAddress: 'Adresse du portefeuille',
    note: 'Note',
    optional: 'facultatif',
    notePlaceholder: 'ex. Multisig de l\'équipe',
    saveContact: 'Enregistrer le contact',
    updateContact: 'Mettre à jour le contact',
    deleteContact: 'Supprimer le contact',
    deleteConfirm: 'Cela supprimera définitivement ce contact de votre carnet d\'adresses.',
    noContacts: 'Aucun contact pour le moment',
    noContactsHint: 'Enregistrez des adresses pour un envoi rapide',
    searchPlaceholder: 'Rechercher des contacts\u2026',
    noResults: 'Aucun résultat pour',
    nameRequired: 'Le nom est obligatoire',
    addressRequired: 'L\'adresse est obligatoire',
    invalidAddress: 'Adresse Ethereum invalide',
    contactAdded: 'ajouté',
    contactUpdated: 'mis à jour',
  },
  account: {
    title: 'Compte',
    switchAccount: 'Changer de compte',
  },
} as const;

const de: MessageCatalog = {
  common: {
    ok: 'OK',
    cancel: 'Abbrechen',
    close: 'Schließen',
    back: 'Zurück',
    retry: 'Erneut versuchen',
    done: 'Fertig',
    confirm: 'Bestätigen',
    deny: 'Ablehnen',
    loading: 'Wird geladen\u2026',
    error: 'Etwas ist schiefgelaufen',
    success: 'Erfolg',
    warning: 'Warnung',
    offline: 'Sie sind offline',
    copy: 'Kopieren',
    copied: 'Kopiert',
    search: 'Suchen',
    noResults: 'Keine Ergebnisse',
    learnMore: 'Mehr erfahren',
  },
  nav: {
    portfolio: 'Portfolio',
    send: 'Senden',
    receive: 'Empfangen',
    swap: 'Tauschen',
    discover: 'Entdecken',
    settings: 'Einstellungen',
    dappBrowser: 'dApps',
    contacts: 'Kontakte',
    activity: 'Aktivität',
    ramp: 'Kaufen & Verkaufen',
  },
  shell: {
    walletLocked: 'Wallet gesperrt',
    unlockWallet: 'Wallet entsperren',
    lockWallet: 'Wallet sperren',
    switchAccount: 'Konto wechseln',
    switchNetwork: 'Netzwerk wechseln',
    connectedDapps: 'Verbundene dApps',
    permissions: 'Berechtigungen',
    revokePermission: 'Berechtigung widerrufen',
    disconnectAll: 'Alle trennen',
  },
  settings: {
    title: 'Einstellungen',
    general: 'Allgemein',
    security: 'Sicherheit',
    advanced: 'Erweitert',
    language: 'Sprache',
    currency: 'Währung',
    notifications: 'Benachrichtigungen',
    about: 'Über',
    version: 'Version',
    network: 'Netzwerk',
    customRpc: 'Benutzerdefiniertes RPC',
    developerMode: 'Entwicklermodus',
    showHexData: 'Hex-Daten anzeigen',
    backupWallet: 'Wallet sichern',
    exportPrivateKey: 'Privaten Schlüssel exportieren',
    resetWallet: 'Wallet zurücksetzen',
    manageContacts: 'Kontakte verwalten',
    accounts: 'Konten',
    addAccount: 'Konto hinzufügen',
    deriveNext: 'Nächstes HD-Konto ableiten',
    importPrivateKey: 'Privaten Schlüssel importieren',
    importKeySubtitle: 'Einzelnes Konto per Schlüssel hinzufügen',
    lockWallet: 'Wallet sperren',
    exportRecoveryPhrase: 'Wiederherstellungsphrase exportieren',
    hideRecoveryPhrase: 'Wiederherstellungsphrase verbergen',
    hidePrivateKey: 'Privaten Schlüssel verbergen',
    exportPkSubtitle: 'Privaten Schlüssel des aktiven Kontos exportieren',
    viewOnExplorer: 'Im Explorer anzeigen',
    addressBook: 'Adressbuch',
    contactsSubtitle: 'Gespeicherte Wallet-Adressen verwalten',
    displayPreferences: 'Anzeige-Einstellungen',
    networkRpc: 'Netzwerk & RPC',
    advancedSubtitle: 'Nonce, Gas, Entwicklertools',
    notificationsSubtitle: 'Preisalarme, Transaktionsupdates',
    connectedDappsSubtitle: 'dApp-Sitzungen verwalten',
    walletMode: 'Wallet-Modus',
    switchWalletMode: 'Wallet-Modus wechseln',
    selfCustodySubtitle: 'Zu Self-Custody Wallet wechseln',
    managedSubtitle: 'Zu verwaltetem Wallet wechseln',
    dangerZone: 'Gefahrenzone',
    eraseWallet: 'Wallet löschen',
    eraseConfirm: 'Erneut antippen, um das Wallet zu löschen',
    freshAuth: 'Authentifizierung erforderlich',
    freshAuthMnemonic: 'Geben Sie Ihre PIN ein, um die Wiederherstellungsphrase zu exportieren.',
    freshAuthPk: 'Geben Sie Ihre PIN ein, um den privaten Schlüssel zu exportieren.',
    verificationFailed: 'Verifizierung fehlgeschlagen',
    pkWarning: 'Teilen Sie niemals Ihren privaten Schlüssel. Jeder damit kann Ihre Mittel stehlen.',
    copyPk: 'Privaten Schlüssel kopieren',
    copyPhrase: 'Phrase kopieren',
    signedInAs: 'Angemeldet als',
    signOut: 'Abmelden',
    signOutSubtitle: 'Ihre Sitzung auf diesem Gerät beenden',
    switchToFunded: 'Zu funded-Konto wechseln',
    switchToFundedSubtitle: 'Eigenkapital, Drawdown und Stufe anzeigen',
    becomeFunded: 'Funded Trader werden',
    becomeFundedSubtitle: 'Handeln Sie mit funded Kapital. Keine Einzahlung, Gewinnbeteiligung gilt.',
    switchToStandard: 'Zum Standard-Wallet wechseln',
    switchToStandardSubtitle: 'Ihr verwaltetes Standard-Wallet anzeigen',
    managedCustody: 'verwahrte Verwaltung',
    customNonce: 'Benutzerdefiniertes Nonce',
    customNoncePlaceholder: 'Leer lassen für automatisch',
    customNonceWarning: 'Setzen Sie dies nur, wenn Sie die Nonce-Reihenfolge verstehen. Ein falsches Nonce kann zum Scheitern einer Transaktion führen.',
    saveRpc: 'RPC speichern',
    saved: 'Gespeichert',
    rpcPlaceholder: 'https://rpc.example.com',
    rpcHint: 'Leer lassen, um den Standard-RPC-Knoten zu verwenden',
    displayInputData: 'Eingabedaten in Transaktionen anzeigen',
    debugInformation: 'Rohdaten und Debug-Informationen anzeigen',
    noConnectedDapps: 'Keine verbundenen dApps',
    noConnectedDappsHint: 'dApps, die Sie im Browser besuchen, werden hier angezeigt',
    lastVisited: 'Zuletzt besucht',
    revoke: 'Widerrufen',
    revokeAll: 'Alle Sitzungen widerrufen',
  },
  tx: {
    send: 'Senden',
    confirm: 'Transaktion bestätigen',
    approving: 'Genehmigung\u2026',
    submitting: 'Wird gesendet\u2026',
    submitted: 'Gesendet',
    confirmed: 'Bestätigt',
    failed: 'Fehlgeschlagen',
    pending: 'Ausstehend',
    speedUp: 'Beschleunigen',
    cancelTx: 'Transaktion abbrechen',
    nonce: 'Nonce',
    gasFee: 'Netzwerkgebühr',
    total: 'Gesamt',
    to: 'An',
    from: 'Von',
    amount: 'Betrag',
    balance: 'Guthaben',
    max: 'Max',
    insufficientFunds: 'Unzureichende Mittel',
    invalidAddress: 'Ungültige Adresse',
  },
  approval: {
    requestFrom: 'Anfrage von',
    network: 'Netzwerk',
    operation: 'Vorgang',
    risk: 'Risiko',
    simulation: 'Simulation',
    details: 'Details',
    rawData: 'Rohdaten',
    expirWarning: 'Diese Anfrage läuft bald ab',
    highRisk: 'Hohes Risiko',
    unknownToken: 'Unbekanntes Token',
    infiniteApproval: 'Unendliche Genehmigung',
  },
  swap: {
    title: 'Tausch',
    from: 'Sie zahlen',
    to: 'Sie erhalten',
    rate: 'Kurs',
    priceImpact: 'Preisauswirkung',
    slippage: 'Slippage-Toleranz',
    minimumReceived: 'Minimum erhalten',
    networkFee: 'Netzwerkgebühr',
    route: 'Route',
    quoteExpired: 'Angebot abgelaufen',
    fetchingQuote: 'Angebot wird geladen\u2026',
    approveToken: 'Token genehmigen',
    executeSwap: 'Tausch ausführen',
    noRoutes: 'Keine Routen verfügbar',
  },
  a11y: {
    openMenu: 'Menü öffnen',
    closeMenu: 'Menü schließen',
    openDialog: 'Dialog öffnen',
    closeDialog: 'Dialog schließen',
    nextPage: 'Nächste Seite',
    previousPage: 'Vorherige Seite',
    showMore: 'Mehr anzeigen',
    showLess: 'Weniger anzeigen',
    flipTokens: 'Token vertauschen',
    scanQr: 'QR-Code scannen',
    copyAddress: 'Adresse kopieren',
    goToPage: 'Zur Seite',
    currentPage: 'Aktuelle Seite',
  },
  receive: {
    title: 'Empfangen',
    yourAddress: 'Ihre Adresse',
    addressCopied: 'Adresse kopiert',
    share: 'Teilen',
    shareTitle: 'Wallet-Adresse',
  },
  contacts: {
    title: 'Kontakte',
    addContact: 'Kontakt hinzufügen',
    editContact: 'Kontakt bearbeiten',
    newContact: 'Neuer Kontakt',
    name: 'Name',
    namePlaceholder: 'z.B. Alice',
    walletAddress: 'Wallet-Adresse',
    note: 'Notiz',
    optional: 'optional',
    notePlaceholder: 'z.B. Team-Multisig',
    saveContact: 'Kontakt speichern',
    updateContact: 'Kontakt aktualisieren',
    deleteContact: 'Kontakt löschen',
    deleteConfirm: 'Dies wird den Kontakt dauerhaft aus Ihrem Adressbuch entfernen.',
    noContacts: 'Noch keine Kontakte',
    noContactsHint: 'Speichern Sie Wallet-Adressen für schnelles Senden',
    searchPlaceholder: 'Kontakte suchen\u2026',
    noResults: 'Keine Ergebnisse für',
    nameRequired: 'Name ist erforderlich',
    addressRequired: 'Adresse ist erforderlich',
    invalidAddress: 'Ungültige Ethereum-Adresse',
    contactAdded: 'hinzugefügt',
    contactUpdated: 'aktualisiert',
  },
  account: {
    title: 'Konto',
    switchAccount: 'Konto wechseln',
  },
} as const;

const pt: MessageCatalog = {
  common: {
    ok: 'OK',
    cancel: 'Cancelar',
    close: 'Fechar',
    back: 'Voltar',
    retry: 'Tentar novamente',
    done: 'Concluído',
    confirm: 'Confirmar',
    deny: 'Recusar',
    loading: 'Carregando\u2026',
    error: 'Algo deu errado',
    success: 'Sucesso',
    warning: 'Aviso',
    offline: 'Você está offline',
    copy: 'Copiar',
    copied: 'Copiado',
    search: 'Pesquisar',
    noResults: 'Sem resultados',
    learnMore: 'Saiba mais',
  },
  nav: {
    portfolio: 'Portfólio',
    send: 'Enviar',
    receive: 'Receber',
    swap: 'Trocar',
    discover: 'Descobrir',
    settings: 'Configurações',
    dappBrowser: 'dApps',
    contacts: 'Contatos',
    activity: 'Atividade',
    ramp: 'Comprar e Vender',
  },
  shell: {
    walletLocked: 'Carteira bloqueada',
    unlockWallet: 'Desbloquear carteira',
    lockWallet: 'Bloquear carteira',
    switchAccount: 'Trocar conta',
    switchNetwork: 'Trocar rede',
    connectedDapps: 'dApps conectadas',
    permissions: 'Permissões',
    revokePermission: 'Revogar permissão',
    disconnectAll: 'Desconectar todos',
  },
  settings: {
    title: 'Configurações',
    general: 'Geral',
    security: 'Segurança',
    advanced: 'Avançado',
    language: 'Idioma',
    currency: 'Moeda',
    notifications: 'Notificações',
    about: 'Sobre',
    version: 'Versão',
    network: 'Rede',
    customRpc: 'RPC personalizado',
    developerMode: 'Modo desenvolvedor',
    showHexData: 'Mostrar dados hex',
    backupWallet: 'Fazer backup da carteira',
    exportPrivateKey: 'Exportar chave privada',
    resetWallet: 'Redefinir carteira',
    manageContacts: 'Gerenciar contatos',
    accounts: 'Contas',
    addAccount: 'Adicionar conta',
    deriveNext: 'Derivar próxima conta HD',
    importPrivateKey: 'Importar chave privada',
    importKeySubtitle: 'Adicionar conta única por chave',
    lockWallet: 'Bloquear carteira',
    exportRecoveryPhrase: 'Exportar frase de recuperação',
    hideRecoveryPhrase: 'Ocultar frase de recuperação',
    hidePrivateKey: 'Ocultar chave privada',
    exportPkSubtitle: 'Exportar chave privada da conta ativa',
    viewOnExplorer: 'Ver no explorador',
    addressBook: 'Catálogo de endereços',
    contactsSubtitle: 'Gerenciar endereços salvos',
    displayPreferences: 'Preferências de exibição',
    networkRpc: 'Rede e RPC',
    advancedSubtitle: 'Nonce, gas, ferramentas de desenvolvimento',
    notificationsSubtitle: 'Alertas de preço, atualizações de tx',
    connectedDappsSubtitle: 'Gerenciar sessões de dApps',
    walletMode: 'Modo da carteira',
    switchWalletMode: 'Alternar modo da carteira',
    selfCustodySubtitle: 'Mudar para carteira de autocustódia',
    managedSubtitle: 'Mudar para carteira gerenciada',
    dangerZone: 'Zona de perigo',
    eraseWallet: 'Apagar carteira',
    eraseConfirm: 'Toque novamente para apagar a carteira',
    freshAuth: 'Autenticação necessária',
    freshAuthMnemonic: 'Digite seu PIN para exportar a frase de recuperação.',
    freshAuthPk: 'Digite seu PIN para exportar a chave privada.',
    verificationFailed: 'Falha na verificação',
    pkWarning: 'Nunca compartilhe sua chave privada. Qualquer pessoa com ela pode roubar seus fundos.',
    copyPk: 'Copiar chave privada',
    copyPhrase: 'Copiar frase',
    signedInAs: 'Conectado como',
    signOut: 'Sair',
    signOutSubtitle: 'Encerrar sua sessão neste dispositivo',
    switchToFunded: 'Mudar para conta financiada',
    switchToFundedSubtitle: 'Ver seu capital, drawdown e nível',
    becomeFunded: 'Tornar-se trader financiado',
    becomeFundedSubtitle: 'Negocie com capital financiado. Sem depósito, compartilhamento de lucros se aplica.',
    switchToStandard: 'Mudar para carteira padrão',
    switchToStandardSubtitle: 'Ver sua carteira gerenciada padrão',
    managedCustody: 'custódia gerenciada',
    customNonce: 'Nonce personalizado',
    customNoncePlaceholder: 'Deixe em branco para automático',
    customNonceWarning: 'Defina isso apenas se entende a ordenação de nonces. Um nonce incorreto pode causar falha na transação.',
    saveRpc: 'Salvar RPC',
    saved: 'Salvo',
    rpcPlaceholder: 'https://rpc.exemplo.com',
    rpcHint: 'Deixe em branco para usar o nó RPC padrão',
    displayInputData: 'Exibir dados de entrada nas transações',
    debugInformation: 'Exibir dados brutos e informações de depuração',
    noConnectedDapps: 'Nenhuma dApp conectada',
    noConnectedDappsHint: 'As dApps que você visita no navegador aparecerão aqui',
    lastVisited: 'Última visita',
    revoke: 'Revogar',
    revokeAll: 'Revogar todas as sessões',
  },
  tx: {
    send: 'Enviar',
    confirm: 'Confirmar transação',
    approving: 'Aprovando\u2026',
    submitting: 'Enviando\u2026',
    submitted: 'Enviada',
    confirmed: 'Confirmada',
    failed: 'Falhou',
    pending: 'Pendente',
    speedUp: 'Acelerar',
    cancelTx: 'Cancelar transação',
    nonce: 'Nonce',
    gasFee: 'Taxa de rede',
    total: 'Total',
    to: 'Para',
    from: 'De',
    amount: 'Valor',
    balance: 'Saldo',
    max: 'Máx',
    insufficientFunds: 'Fundos insuficientes',
    invalidAddress: 'Endereço inválido',
  },
  approval: {
    requestFrom: 'Solicitação de',
    network: 'Rede',
    operation: 'Operação',
    risk: 'Risco',
    simulation: 'Simulação',
    details: 'Detalhes',
    rawData: 'Dados brutos',
    expirWarning: 'Esta solicitação expira em breve',
    highRisk: 'Alto risco',
    unknownToken: 'Token desconhecido',
    infiniteApproval: 'Aprovação infinita',
  },
  swap: {
    title: 'Troca',
    from: 'Você paga',
    to: 'Você recebe',
    rate: 'Taxa',
    priceImpact: 'Impacto no preço',
    slippage: 'Tolerância de slippage',
    minimumReceived: 'Mínimo recebido',
    networkFee: 'Taxa de rede',
    route: 'Rota',
    quoteExpired: 'Cotação expirada',
    fetchingQuote: 'Buscando cotação\u2026',
    approveToken: 'Aprovar token',
    executeSwap: 'Executar troca',
    noRoutes: 'Nenhuma rota disponível',
  },
  a11y: {
    openMenu: 'Abrir menu',
    closeMenu: 'Fechar menu',
    openDialog: 'Abrir diálogo',
    closeDialog: 'Fechar diálogo',
    nextPage: 'Próxima página',
    previousPage: 'Página anterior',
    showMore: 'Mostrar mais',
    showLess: 'Mostrar menos',
    flipTokens: 'Inverter tokens',
    scanQr: 'Escanear código QR',
    copyAddress: 'Copiar endereço',
    goToPage: 'Ir para a página',
    currentPage: 'Página atual',
  },
  receive: {
    title: 'Receber',
    yourAddress: 'Seu endereço',
    addressCopied: 'Endereço copiado',
    share: 'Compartilhar',
    shareTitle: 'Endereço da carteira',
  },
  contacts: {
    title: 'Contatos',
    addContact: 'Adicionar contato',
    editContact: 'Editar contato',
    newContact: 'Novo contato',
    name: 'Nome',
    namePlaceholder: 'ex. Alice',
    walletAddress: 'Endereço da carteira',
    note: 'Nota',
    optional: 'opcional',
    notePlaceholder: 'ex. Multisig da equipe',
    saveContact: 'Salvar contato',
    updateContact: 'Atualizar contato',
    deleteContact: 'Excluir contato',
    deleteConfirm: 'Isso removerá permanentemente este contato do seu catálogo de endereços.',
    noContacts: 'Nenhum contato ainda',
    noContactsHint: 'Salve endereços de carteira para envio rápido',
    searchPlaceholder: 'Pesquisar contatos\u2026',
    noResults: 'Nenhum resultado para',
    nameRequired: 'O nome é obrigatório',
    addressRequired: 'O endereço é obrigatório',
    invalidAddress: 'Endereço Ethereum inválido',
    contactAdded: 'adicionado',
    contactUpdated: 'atualizado',
  },
  account: {
    title: 'Conta',
    switchAccount: 'Trocar conta',
  },
} as const;

export const MESSAGE_CATALOGS: Readonly<Record<Locale, MessageCatalog>> = {
  en,
  es,
  fr,
  de,
  pt,
  ar,
  it,
  hi,
  ja,
  ko,
};

export function getMessages(locale: Locale): MessageCatalog {
  return MESSAGE_CATALOGS[locale] ?? MESSAGE_CATALOGS.en;
}
