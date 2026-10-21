import type {Metadata} from 'next';
import {notFound} from 'next/navigation';
import PnlCardLanding from '@/widgets/pnl/pnl-card-landing';
import {PublicShareError,readPublicShare} from '@/widgets/pnl/share-transport';
import {checkedPublicShare} from '@/widgets/pnl/share-disclosure';
interface PageProps{params:Promise<{cardId:string}>}
export const dynamic='force-dynamic';
export const revalidate=0;
export async function generateMetadata({params}:PageProps):Promise<Metadata>{const {cardId}=await params;try{const share=checkedPublicShare(await readPublicShare(cardId));return{title:'PnL card — Sidiora',description:'Trading results shared on Sidiora',robots:{index:false,follow:false},openGraph:{title:'PnL card — Sidiora',images:[{url:`/api/og/pnl/${share.id}`,width:1200,height:630}]}};}catch{return{title:'Card not found — Sidiora',robots:{index:false,follow:false}};}}
export default async function PnlCardPage({params}:PageProps){const {cardId}=await params;try{const share=checkedPublicShare(await readPublicShare(cardId));return <PnlCardLanding share={share}/>;}catch(error){if(error instanceof PublicShareError&&error.status===404)notFound();if(error instanceof PublicShareError&&error.status===410||error instanceof Error&&error.message==='Card withdrawn or expired')return <div className="p-6 text-white"><h1>This card is gone</h1><p>It was removed or has expired</p></div>;return <div className="p-6 text-white"><h1>Can&apos;t load this card right now</h1><p>Try again in a moment</p></div>;}}
