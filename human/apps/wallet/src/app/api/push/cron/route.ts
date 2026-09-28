import { NextRequest, NextResponse } from 'next/server';
import {
  processPendingCampaigns,
  sendInactivityNudges,
} from '@/server/push-service';
import {
  addCampaign,
  getAllCampaigns,
  parseCampaign,
} from '@/server/push-store';
import {
  boundaryErrorResponse,
  HttpBoundaryError,
  readBoundedJson,
  requirePushAdmin,
} from '@/server/http';

export async function GET(request: NextRequest) {
  try {
    requirePushAdmin(request);
    const [campaigns, inactivity] = await Promise.all([
      processPendingCampaigns(),
      sendInactivityNudges(3 * 24 * 60 * 60 * 1000),
    ]);
    return NextResponse.json(
      {
        ok: true,
        campaigns,
        inactivity,
        timestamp: new Date().toISOString(),
      },
      { headers: { 'Cache-Control': 'no-store' } },
    );
  } catch (error) {
    return boundaryErrorResponse(error);
  }
}

export async function POST(request: NextRequest) {
  try {
    requirePushAdmin(request);
    const raw = await readBoundedJson(request, 65_536);
    if (typeof raw !== 'object' || raw === null || Array.isArray(raw)) {
      throw new HttpBoundaryError(400, 'CAMPAIGN_INVALID', 'Campaign is invalid');
    }
    const body = raw as Record<string, unknown>;
    const campaign = parseCampaign({
      ...body,
      url: body.url ?? '/',
    });
    if (campaign.url && (!campaign.url.startsWith('/') || campaign.url.startsWith('//'))) {
      throw new HttpBoundaryError(400, 'CAMPAIGN_INVALID', 'Campaign is invalid');
    }
    await addCampaign(campaign);
    return NextResponse.json(
      { ok: true, campaign: campaign.id },
      { headers: { 'Cache-Control': 'no-store' } },
    );
  } catch (error) {
    return boundaryErrorResponse(error);
  }
}

export async function HEAD(request: NextRequest) {
  try {
    requirePushAdmin(request);
    const campaigns = await getAllCampaigns();
    return new NextResponse(null, {
      status: 200,
      headers: {
        'Cache-Control': 'no-store',
        'X-Campaign-Count': String(campaigns.length),
      },
    });
  } catch (error) {
    return boundaryErrorResponse(error);
  }
}
