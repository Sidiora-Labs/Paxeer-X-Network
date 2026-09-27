import React from 'react';

import { route } from 'nextjs/routes';

import config from 'configs/app';
import useIsMobile from 'lib/hooks/useIsMobile';
import { Link } from 'toolkit/chakra/link';
import IconSvg from 'ui/shared/IconSvg';
import PageTitle from 'ui/shared/Page/PageTitle';
import TxsStats from 'ui/txs/TxsStats';
import TxsTabs from 'ui/txs/TxsTabs';

const TAB_LIST_PROPS = {
  marginBottom: 0,
  pt: 6,
  pb: 6,
  marginTop: -5,
};
const TABS_HEIGHT = 88;

const Transactions = () => {
  const isMobile = useIsMobile();

  const apiEntry = config.features.apiDocs.isEnabled ? (
    <Link
      href={ route({ pathname: '/api-docs' }) }
      textStyle="sm"
      display="inline-flex"
      alignItems="center"
      columnGap={ 1 }
      data-page-api-entry
    >
      <IconSvg name="API" boxSize={ 4 }/>
      API
    </Link>
  ) : null;

  return (
    <>
      <PageTitle
        title={ config.meta.seo.enhancedDataEnabled ? `${ config.chain.name } transactions` : 'Transactions' }
        afterTitle={ apiEntry }
        withTextAd
      />
      <TxsStats/>
      <TxsTabs
        listProps={ isMobile ? undefined : TAB_LIST_PROPS }
        tabsHeight={ TABS_HEIGHT }
      />
    </>
  );
};

export default Transactions;
