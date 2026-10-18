'use client';
import { DashboardBoard, useCompactHome } from '@/widgets/home/dashboard-board';
import { HomeFeed } from '@/widgets/home/home-feed';

/** Home: the column board from lg, the single callouts feed below it (pump.fun 768 / 390). */
export default function HomeModule() {
  const compact = useCompactHome();
  return (
    <>
      <h1 className="sr-only">Sidiora dashboard</h1>
      {compact ? <HomeFeed /> : <DashboardBoard />}
    </>
  );
}
