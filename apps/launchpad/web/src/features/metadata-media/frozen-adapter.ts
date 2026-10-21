import type {ImageOutcome} from './editor';
import {revisionValid,approvedOutcome} from './outcome';
// Structural frozen Asset subset. HTTP envelope validation is the SDK boundary;
// operation correlation is local authenticated request context, not a new field.
export type FrozenAsset={assetId:string;ownerWallet:string;sha256:string;mediaType:'image/png'|'image/jpeg'|'image/webp';bytes:number;state:'uploading'|'quarantined'|'approved'|'rejected';publicUrl:string|null};
export type FrozenMetadataLink={revision:string;token:{logoAssetId:string|null;metadataRevision:string}};
export function projectFrozenImage(asset:FrozenAsset,operationId:string,priorRevision:string,metadata:FrozenMetadataLink|null):ImageOutcome{
 if(!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(asset.assetId)||!/^0x[0-9a-fA-F]{40}$/.test(asset.ownerWallet)||!/^0x[0-9a-fA-F]{64}$/.test(asset.sha256)||!Number.isInteger(asset.bytes)||asset.bytes<1||asset.bytes>6291456||!['image/png','image/jpeg','image/webp'].includes(asset.mediaType))throw new Error('Invalid asset contract');
 if(asset.state==='rejected')throw new Error('Image rejected');
 if(asset.state!=='approved'){if(asset.publicUrl!==null||!['uploading','quarantined'].includes(asset.state))throw new Error('Invalid asset state');return {operationId,assetId:asset.assetId,state:'QUARANTINED'}}
 // Approved bytes alone do not imply the creator metadata revision has changed.
 if(!metadata||metadata.token.logoAssetId!==asset.assetId)return {operationId,assetId:asset.assetId,state:'VALIDATING'};
 if(!revisionValid(metadata.revision)||metadata.token.metadataRevision!==metadata.revision||!asset.publicUrl)throw new Error('Invalid metadata link');
 if(!new URL(asset.publicUrl).pathname.startsWith('/approved/'+asset.sha256.slice(2).toLowerCase()+'/'))throw new Error('Foreign asset content');
 const out:ImageOutcome={operationId,assetId:asset.assetId,state:'APPROVED',revision:metadata.revision,url:asset.publicUrl};if(!approvedOutcome(out,operationId,priorRevision))throw new Error('Unconfirmed image revision');return out;
}

export type V2MetadataLink={revision:string;token:{logoAssetId:string|null;metadataRevision:string}};
export function projectV2Image(asset:FrozenAsset,operationId:string,priorRevision:string,metadata:V2MetadataLink|null):ImageOutcome{
 return projectFrozenImage(asset,operationId,priorRevision,metadata);
}
