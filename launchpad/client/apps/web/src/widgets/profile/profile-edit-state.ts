import type { ExtendedProfileBody } from './account-transport';
import { uint256Revision } from './account-disclosure';
import { profileMutation, type ProfileDraft } from './account-disclosure';
import { AccountApiError, patchProfile } from './account-transport';
export type ProfileSaveResult = { kind: 'committed'; revision: string } | { kind: 'conflict'; draft: ProfileDraft };
export async function commitProfile(wallet: string, expectedRevision: string, draft: ProfileDraft): Promise<ProfileSaveResult> {
  try {
    const response = await patchProfile(wallet, profileMutation(expectedRevision, draft));
    if (BigInt(response.data.profile.revision) <= BigInt(expectedRevision)) throw new Error('Profile acknowledgment did not advance revision');
    return { kind: 'committed', revision: response.data.profile.revision };
  } catch (error) {
    if (error instanceof AccountApiError && error.status === 412) return { kind: 'conflict', draft: { ...draft } };
    throw error;
  }
}

export function validatedExtendedProfile(body: ExtendedProfileBody): ExtendedProfileBody {
  uint256Revision(body.expectedRevision);
  const fields: { pointer: string; rule: string }[] = [];
  const next: ExtendedProfileBody = { expectedRevision: body.expectedRevision };
  if (body.displayName !== undefined) {
    const displayName = body.displayName.trim().normalize('NFC');
    if ([...displayName].length < 1 || [...displayName].length > 50) fields.push({pointer:'/displayName',rule:'Use 1 to 50 characters'});
    next.displayName = displayName;
  }
  if (body.bio !== undefined) { if ([...body.bio].length > 500) fields.push({pointer:'/bio',rule:'Keep it under 500 characters'}); next.bio=body.bio; }
  for (const key of ['avatarAssetId','bannerAssetId'] as const) {
    const value=body[key];
    if(value!==undefined){if(value!==null&&(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value)||key==='bannerAssetId'&&value==='00000000-0000-0000-0000-000000000000'))fields.push({pointer:`/${key}`,rule:'Pick an image to upload'});next[key]=value;}
  }
  function link(value:string|null|undefined,pointer:string){
    if(value==null)return;
    try{const url=new URL(value);if(new TextEncoder().encode(value).length>2048||url.protocol!=='https:'||!url.hostname||url.username||url.password||/[ \r\n\t]/.test(value))throw new Error();}
    catch{fields.push({pointer,rule:'Use a full link starting with https://'});}
  }
  if(body.website!==undefined){link(body.website,'/website');next.website=body.website;}
  if(body.socials!==undefined){next.socials={};for(const key of ['website','twitter','telegram','discord'] as const){const value=body.socials[key];if(value!==undefined){link(value,`/socials/${key}`);next.socials[key]=value;}}}
  if(fields.length)throw new AccountApiError(400,'PROFILE_VALIDATION',fields);
  return next;
}
