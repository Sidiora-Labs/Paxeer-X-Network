import type { GridProps, HTMLChakraProps } from '@chakra-ui/react';
import { Box, Grid, Flex, Text, VStack } from '@chakra-ui/react';
import { useQuery } from '@tanstack/react-query';
import React from 'react';

import type { CustomLinksGroup } from 'types/footerLinks';

import { route } from 'nextjs-routes';

import config from 'configs/app';
import type { ResourceError } from 'lib/api/resources';
import useApiQuery from 'lib/api/useApiQuery';
import useFetch from 'lib/hooks/useFetch';
import { Button } from 'toolkit/chakra/button';
import { Link } from 'toolkit/chakra/link';
import { Skeleton } from 'toolkit/chakra/skeleton';
import { copy } from 'toolkit/utils/htmlEntities';
import type { IconName } from 'ui/shared/IconSvg';
import IconSvg from 'ui/shared/IconSvg';
import { CONTENT_MAX_WIDTH } from 'ui/shared/layout/utils';
import NetworkAddToWallet from 'ui/shared/NetworkAddToWallet';
import NetworkLogo from 'ui/snippets/networkLogo/NetworkLogo';

import FooterLinkItem from './FooterLinkItem';
import IntTxsIndexingStatus from './IntTxsIndexingStatus';

const MAX_LINKS_COLUMNS = 3;

const SOCIAL_ICONS: Array<{ hosts: Array<string>; icon: IconName }> = [
  { hosts: [ 'x.com', 'twitter.com' ], icon: 'social/twitter' },
  { hosts: [ 'github.com' ], icon: 'social/git' },
  { hosts: [ 'discord.gg', 'discord.com' ], icon: 'social/discord' },
  { hosts: [ 't.me', 'telegram.me' ], icon: 'social/telega' },
];

const getSocialIcon = (url: string): IconName | undefined => {
  const host = url.match(/^https?:\/\/([^/?#]+)/i)?.[1]?.toLowerCase().replace(/^www\./, '');

  if (!host) {
    return undefined;
  }

  return SOCIAL_ICONS.find((item) => item.hosts.includes(host))?.icon;
};

const Footer = () => {

  const { data: backendVersionData } = useApiQuery('general:config_backend_version', {
    queryOptions: {
      staleTime: Infinity,
      enabled: !config.features.multichain.isEnabled,
      refetchOnMount: false,
    },
  });

  const fetch = useFetch();

  const { isPlaceholderData, data: linksData } = useQuery<unknown, ResourceError<unknown>, Array<CustomLinksGroup>>({
    queryKey: [ 'footer-links' ],
    queryFn: async() => fetch(config.UI.footer.links || '', undefined, { resource: 'footer-links' }),
    enabled: Boolean(config.UI.footer.links),
    staleTime: Infinity,
    placeholderData: [],
  });

  const isLinksLoading = Boolean(config.UI.footer.links) && isPlaceholderData;

  const fixedLinkGroups: Array<CustomLinksGroup> = React.useMemo(() => {
    return [
      {
        title: 'Explore',
        links: [
          { text: 'Blocks', url: route({ pathname: '/blocks' }) },
          { text: 'Transactions', url: route({ pathname: '/txs' }) },
          { text: 'Tokens', url: route({ pathname: '/tokens' }) },
          !config.UI.views.address.hiddenViews?.top_accounts && { text: 'Top accounts', url: route({ pathname: '/accounts' }) },
        ].filter(Boolean),
      },
      {
        title: 'Network',
        links: [
          config.features.stats.isEnabled && { text: 'Chain stats', url: route({ pathname: '/stats' }) },
          config.features.gasTracker.isEnabled && { text: 'Gas tracker', url: route({ pathname: '/gas-tracker' }) },
          config.features.validators.isEnabled && { text: 'Validators', url: route({ pathname: '/validators' }) },
          { text: 'Verified contracts', url: route({ pathname: '/verified-contracts' }) },
        ].filter(Boolean),
      },
      {
        title: 'Developers',
        links: [
          config.features.apiDocs.isEnabled && { text: 'API docs', url: route({ pathname: '/api-docs' }) },
          { text: 'Verify contract', url: route({ pathname: '/contract-verification' }) },
          config.features.advancedFilter.isEnabled && { text: 'Advanced filter', url: route({ pathname: '/advanced-filter' }) },
        ].filter(Boolean),
      },
    ];
  }, []);

  const linkColumns = React.useMemo(() => {
    const configGroups = (linksData || []).map((group) => ({ group, isExternal: true }));
    const fixedGroups = fixedLinkGroups.map((group) => ({ group, isExternal: false }));

    return [ ...configGroups, ...fixedGroups ].slice(0, MAX_LINKS_COLUMNS);
  }, [ linksData, fixedLinkGroups ]);

  const handleBackToTop = React.useCallback(() => {
    window.scrollTo({ top: 0, behavior: 'smooth' });
  }, []);

  const containerProps: HTMLChakraProps<'div'> = {
    as: 'footer',
    borderTopWidth: '1px',
    borderTopColor: 'border.divider',
    bgColor: 'bg.primary',
  };

  const contentProps: HTMLChakraProps<'div'> = {
    px: { base: 4, lg: config.UI.navigation.layout === 'horizontal' ? 6 : 12, '2xl': 6 },
    py: { base: 6, lg: 8 },
    maxW: `${ CONTENT_MAX_WIDTH }px`,
    m: '0 auto',
  };

  const columnsProps: GridProps = {
    gridTemplateColumns: { base: '1fr', lg: 'minmax(auto, 360px) repeat(3, 1fr)' },
    columnGap: { lg: 8, xl: 12 },
    rowGap: 8,
  };

  const renderRecaptcha = () => {
    if (!config.services.reCaptchaV2.siteKey) {
      return null;
    }

    return (
      <Box textStyle="xs" color="text.secondary">
        <span>This site is protected by reCAPTCHA and the Google </span>
        <Link href="https://policies.google.com/privacy" external noIcon>Privacy Policy</Link>
        <span> and </span>
        <Link href="https://policies.google.com/terms" external noIcon>Terms of Service</Link>
        <span> apply.</span>
      </Box>
    );
  };

  return (
    <Box { ...containerProps }>
      <Box { ...contentProps }>
        <Flex
          data-label="footer-top"
          alignItems="center"
          justifyContent="space-between"
          columnGap={ 4 }
          rowGap={ 3 }
          flexWrap="wrap"
          mb={{ base: 6, lg: 8 }}
        >
          <Flex data-label="footer-social" alignItems="center" columnGap={ 4 } rowGap={ 2 } flexWrap="wrap" _empty={{ display: 'none' }}>
            { config.UI.navigation.otherLinks.map((link) => (
              <FooterLinkItem key={ link.text } text={ link.text } url={ link.url } icon={ getSocialIcon(link.url) }/>
            )) }
          </Flex>
          <Button data-label="back-to-top" variant="link" size="sm" onClick={ handleBackToTop } textStyle="xs">
            Back to Top
            <IconSvg name="arrows/east-mini" boxSize={ 5 } transform="rotate(90deg)"/>
          </Button>
        </Flex>

        <Grid { ...columnsProps }>
          <Box data-label="footer-brand">
            <NetworkLogo display="inline-block" mb={ 3 }/>
            <Text textStyle="sm" fontWeight={ 500 } color="heading">{ config.chain.name }</Text>
            <Text mt={ 3 } textStyle="xs" color="text.secondary">
              The block explorer for { config.chain.name }: search blocks, transactions, addresses, tokens
              and kernel activity across the network.
            </Text>
            <Flex mt={ 5 } alignItems="center" flexWrap="wrap" columnGap={ 3 } rowGap={ 2 } _empty={{ display: 'none' }}>
              { !config.UI.indexingAlert.intTxs.isHidden && <IntTxsIndexingStatus/> }
              { !config.features.multichain.isEnabled && <NetworkAddToWallet source="Footer"/> }
            </Flex>
            <Box mt={ 6 } textStyle="xs" color="text.secondary" _empty={{ display: 'none' }}>
              { backendVersionData?.backend_version && <Text>Backend { backendVersionData.backend_version }</Text> }
              { config.UI.footer.frontendVersion && <Text>Frontend { config.UI.footer.frontendVersion }</Text> }
            </Box>
          </Box>

          { linkColumns.map(({ group, isExternal }) => (
            <Box key={ group.title } data-label="footer-column">
              <Skeleton fontWeight={ 500 } mb={ 3 } display="inline-block" loading={ isLinksLoading }>{ group.title }</Skeleton>
              <VStack gap={ 1 } alignItems="start">
                { group.links.map((link) => isExternal ? (
                  <FooterLinkItem { ...link } key={ link.text } isLoading={ isLinksLoading }/>
                ) : (
                  <Link
                    key={ link.text }
                    href={ link.url }
                    display="flex"
                    alignItems="center"
                    h="30px"
                    variant="subtle"
                    textStyle="xs"
                  >
                    { link.text }
                  </Link>
                )) }
              </VStack>
            </Box>
          )) }
        </Grid>

        <Flex
          data-label="footer-bottom"
          mt={{ base: 8, lg: 10 }}
          pt={ 4 }
          borderTopWidth="1px"
          borderTopColor="border.divider"
          alignItems="center"
          justifyContent="space-between"
          columnGap={ 4 }
          rowGap={ 2 }
          flexWrap="wrap"
        >
          <Text textStyle="xs" color="text.secondary">
            { config.chain.name } Block Explorer { copy } { (new Date()).getFullYear() }
          </Text>
          { renderRecaptcha() }
        </Flex>
      </Box>
    </Box>
  );
};

export default React.memo(Footer);
