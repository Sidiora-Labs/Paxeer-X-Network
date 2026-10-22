import type { NextPage } from 'next';
import dynamic from 'next/dynamic';
import React from 'react';

import PageNextJs from 'nextjs/PageNextJs';

const PaxeerXFeed = dynamic(() => import('ui/pages/PaxeerXFeed'), { ssr: false });

const Page: NextPage = () => {
  return (
    <PageNextJs pathname="/feed">
      <PaxeerXFeed/>
    </PageNextJs>
  );
};

export default Page;

export { paxeerXFeed as getServerSideProps } from 'nextjs/getServerSideProps/main';
