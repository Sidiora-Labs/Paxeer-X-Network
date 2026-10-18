import {NativeModules} from 'react-native';
import {localBiometricUnlock,protectedStorage} from './protected-storage.ts';
export async function setPrivatePreview(enabled:boolean){const provider=NativeModules.PrivacyPreview as {setEnabled(value:boolean):Promise<void>}|undefined;if(!provider)throw new Error('Native privacy protection unavailable');await provider.setEnabled(enabled);}
export class LocalPrivacyController {
 enabled=false;unlocked=true;private revision=0;
 constructor(readonly changed:()=>void){}
 async restore(){const saved=await protectedStorage.get('privacy');if(saved){const value:unknown=JSON.parse(saved);if(!value||typeof value!=='object'||!('biometricEnabled'in value)||typeof value.biometricEnabled!=='boolean')throw new Error('Invalid local privacy preference');this.enabled=value.biometricEnabled;this.unlocked=!this.enabled;}this.changed();}
 lock(){this.revision++;if(this.enabled){this.unlocked=false;this.changed();}}
 async unlock(){if(!this.enabled)return;const revision=this.revision;await localBiometricUnlock();if(revision!==this.revision)throw new Error('Application locked during local unlock');this.unlocked=true;this.changed();}
 async setEnabled(enabled:boolean){const revision=this.revision;if(enabled||this.enabled)await localBiometricUnlock();if(revision!==this.revision)throw new Error('Application locked during privacy change');await protectedStorage.set('privacy',JSON.stringify({biometricEnabled:enabled}));this.enabled=enabled;this.unlocked=!enabled||revision===this.revision;this.changed();}
 reset(){this.revision++;this.enabled=false;this.unlocked=true;this.changed();}
}
