import {NextRequest,NextResponse} from 'next/server';
import {pnlApiUrl} from '@/core/sdk-config';
import {validShareId} from '@/widgets/pnl/share-transport';
export const dynamic='force-dynamic';
export const revalidate=0;
type RouteParams={params:Promise<{cardId:string}>};
const headers={'Cache-Control':'no-store','X-Content-Type-Options':'nosniff'};
export async function GET(_request:NextRequest,{params}:RouteParams){const {cardId}=await params;if(!validShareId(cardId))return NextResponse.json({error:'Card not found'},{status:404,headers});try{const upstream=await fetch(pnlApiUrl(`/v3/portfolio/shares/${cardId}/image.png`),{cache:'no-store',credentials:'omit',redirect:'error'});if(!upstream.ok)return NextResponse.json({error:upstream.status===410?'Card withdrawn or expired':'Card unavailable'},{status:upstream.status,headers});if(upstream.headers.get('Content-Type')?.split(';')[0]!=='image/png')return NextResponse.json({error:'Invalid card image'},{status:502,headers});return new NextResponse(upstream.body,{status:200,headers:{...headers,'Content-Type':'image/png'}});}catch{return NextResponse.json({error:'Card image service unavailable'},{status:502,headers});}}
