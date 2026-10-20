import type { NextApiRequest, NextApiResponse } from 'next';

type BuildResponse = {
  buildId: string;
};

export default function handler(
  req: NextApiRequest,
  res: NextApiResponse<BuildResponse>
) {
  const buildId = process.env.RAILWAY_GIT_COMMIT_SHA || process.env.VERCEL_GIT_COMMIT_SHA || 'development';
  res.status(200).json({ buildId });
}