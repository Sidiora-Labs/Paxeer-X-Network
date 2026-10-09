import { chakra, Flex, Text } from '@chakra-ui/react';
import { useQueryClient } from '@tanstack/react-query';
import { pickBy } from 'es-toolkit';
import { useRouter } from 'next/router';
import React from 'react';

import type { PaxeerXReceipt } from 'types/api/paxeerXLists';
import { PAXEER_X_STATUS_RUNGS, PAXEER_X_VERIFICATION_STATUSES } from 'types/api/paxeerXLists';

import { route } from 'nextjs/routes';

import config from 'configs/app';
import useApiQuery, { getResourceKey } from 'lib/api/useApiQuery';
import { useMultichainContext } from 'lib/contexts/multichain';
import throwOnAbsentParamError from 'lib/errors/throwOnAbsentParamError';
import getQueryParamString from 'lib/router/getQueryParamString';
import { Link } from 'toolkit/chakra/link';
import { Skeleton } from 'toolkit/chakra/skeleton';
import PaxeerXReceiptDetails from 'ui/paxeerX/receipts/PaxeerXReceiptDetails';
import TextAd from 'ui/shared/ad/TextAd';
import CopyToClipboard from 'ui/shared/CopyToClipboard';
import IconSvg from 'ui/shared/IconSvg';
import PageTitle from 'ui/shared/Page/PageTitle';
import { ScanSectionTabs } from 'ui/shared/scan';

const apiDocsFeature = config.features.apiDocs;

const TABS = [ { id: 'index', title: 'Overview' } ];

const REFRESH_INTERVAL = 5_000;
const REQUEST_TIMEOUT = 15_000;
const MAX_RETRY_INTERVAL = 30_000;

const terminal = (data: PaxeerXReceipt | undefined) =>
  data?.status === 'final' && data.verification_status === 'settlement_anchored';

const admissible = (next: PaxeerXReceipt, id: string, previous?: PaxeerXReceipt) => {
  if (!next || typeof next.id !== 'string' || next.id.toLowerCase() !== id.toLowerCase() ||
    !PAXEER_X_STATUS_RUNGS.includes(next.status) || !PAXEER_X_VERIFICATION_STATUSES.includes(next.verification_status) ||
    !(next.account === null || typeof next.account === 'string') ||
    !(next.payload_hash === null || typeof next.payload_hash === 'string') ||
    !(next.transaction_hash === null || typeof next.transaction_hash === 'string') ||
    !(next.timestamp === null || (typeof next.timestamp === 'string' && Number.isFinite(Date.parse(next.timestamp)))) ||
    !(next.block_number === null || (Number.isSafeInteger(next.block_number) && next.block_number >= 0))) {
    return false;
  }

  if (!previous) {
    return true;
  }

  return PAXEER_X_STATUS_RUNGS.indexOf(next.status) >= PAXEER_X_STATUS_RUNGS.indexOf(previous.status) &&
    PAXEER_X_VERIFICATION_STATUSES.indexOf(next.verification_status) >= PAXEER_X_VERIFICATION_STATUSES.indexOf(previous.verification_status) &&
    (previous.account === null || next.account === previous.account) &&
    (previous.payload_hash === null || next.payload_hash === previous.payload_hash);
};

const ReceiptContent = ({ id, chainId }: { id: string; chainId?: string }) => {
  const router = useRouter();
  const queryClient = useQueryClient();
  const viewId = React.useId();
  const queryKey = React.useMemo(() => [
    ...getResourceKey('general:paxeer_x_receipt', { pathParams: { id }, chainId }), 'open-detail', viewId,
  ], [ id, chainId, viewId ]);
  const [ availability, setAvailability ] = React.useState({ visible: false, online: true });
  const [ evidence, setEvidence ] = React.useState<{ data: PaxeerXReceipt; receivedAt: number }>();
  const latest = React.useRef(evidence);
  const [ problem, setProblem ] = React.useState<string>();
  const receiptQuery = useApiQuery('general:paxeer_x_receipt', {
    pathParams: { id },
    queryOptions: {
      queryKey,
      enabled: false,
      retry: false,
      staleTime: 0,
      gcTime: 0,
      refetchOnMount: false,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
      throwOnError: false,
    },
  });
  const { refetch } = receiptQuery;
  const isTerminal = terminal(evidence?.data);

  React.useEffect(() => {
    const update = () => setAvailability({ visible: document.visibilityState === 'visible', online: navigator.onLine });
    update();
    document.addEventListener('visibilitychange', update);
    window.addEventListener('online', update);
    window.addEventListener('offline', update);
    return () => {
      document.removeEventListener('visibilitychange', update);
      window.removeEventListener('online', update);
      window.removeEventListener('offline', update);
    };
  }, []);

  React.useEffect(() => {
    if (!availability.visible || !availability.online || isTerminal) {
      return;
    }

    let stopped = false;
    let failures = 0;
    let scheduled: ReturnType<typeof setTimeout> | undefined;
    let deadline: ReturnType<typeof setTimeout> | undefined;
    const refresh = async() => {
      let timedOut = false;
      deadline = setTimeout(() => {
        timedOut = true;
        void queryClient.cancelQueries({ queryKey, exact: true });
      }, REQUEST_TIMEOUT);
      try {
        const result = await refetch({ cancelRefetch: false });
        if (stopped) {
          return;
        }
        if (timedOut || result.isError || !result.data) {
          failures += 1;
          setProblem('Refresh unavailable. Retaining the last indexed evidence.');
        } else if (!admissible(result.data, id, latest.current?.data)) {
          failures += 1;
          setProblem('Conflicting or unsupported receipt evidence. Retaining the last accepted response.');
        } else {
          failures = 0;
          const accepted = { data: result.data, receivedAt: Date.now() };
          latest.current = accepted;
          setEvidence(accepted);
          setProblem(undefined);
        }
      } catch {
        if (!stopped) {
          failures += 1;
          setProblem('Refresh unavailable. Retaining the last indexed evidence.');
        }
      } finally {
        clearTimeout(deadline);
        if (!stopped && !terminal(latest.current?.data)) {
          const delay = Math.min(MAX_RETRY_INTERVAL, REFRESH_INTERVAL * (2 ** Math.min(failures, 3)));
          scheduled = setTimeout(() => {
            void refresh();
          }, delay);
        }
      }
    };
    void refresh();
    return () => {
      stopped = true;
      clearTimeout(scheduled);
      clearTimeout(deadline);
      void queryClient.cancelQueries({ queryKey, exact: true });
    };
  }, [ id, availability.visible, availability.online, isTerminal, queryClient, queryKey, refetch ]);

  const handleTabChange = React.useCallback((value: string) => {
    const queryForPathname = pickBy(router.query, (_, key) => router.pathname.includes(`[${ String(key) }]`));

    router.push(
      { pathname: router.pathname, query: { ...queryForPathname, tab: value } },
      undefined,
      { shallow: true },
    );
  }, [ router ]);

  const data = evidence?.data;
  const isLoading = !data && !problem;
  let freshnessState: 'current' | 'refreshing' | 'stale' | 'paused' | 'complete';
  if (problem || !availability.online) {
    freshnessState = 'stale';
  } else if (isTerminal) {
    freshnessState = 'complete';
  } else if (!availability.visible) {
    freshnessState = 'paused';
  } else if (receiptQuery.isFetching) {
    freshnessState = 'refreshing';
  } else {
    freshnessState = 'current';
  }
  const freshness = {
    state: freshnessState,
    checkedAt: evidence?.receivedAt,
    message: !availability.online ? 'Offline. Retaining the last indexed evidence.' : problem,
  };

  const apiEntry = apiDocsFeature.isEnabled ? (
    <Link href={ route({ pathname: '/api-docs' }) } textStyle="sm" display="inline-flex" alignItems="center" data-api-entry>
      <IconSvg name="API" boxSize={ 4 } mr={ 1 }/>
      API
    </Link>
  ) : null;

  const titleAfter = (
    <Flex alignItems="center" columnGap={ 2 } rowGap={ 2 } flexWrap="wrap" ml={{ base: 0, lg: 3 }} minW={ 0 } data-receipt-identifier>
      <Skeleton loading={ isLoading } overflow="hidden" minW={ 0 }>
        <chakra.span textStyle="lg" color="text.secondary" wordBreak="break-all">{ id }</chakra.span>
      </Skeleton>
      <CopyToClipboard text={ id } isLoading={ isLoading }/>
    </Flex>
  );

  return (
    <>
      <TextAd mb={ 6 }/>
      <PageTitle
        title="Kernel receipt"
        afterTitle={ titleAfter }
        isLoading={ isLoading }
      />
      <Flex flexDir="column" rowGap={{ base: 3, lg: 4 }}>
        <ScanSectionTabs
          items={ TABS }
          value="index"
          onValueChange={ handleTabChange }
          rightSlot={ apiEntry }
        />
        { data ? <PaxeerXReceiptDetails data={ data } isLoading={ isLoading } freshness={ freshness }/> : null }
        { !data && (problem || !availability.online) ? (
          <Text role="alert" color="text.secondary" data-receipt-refresh-error>
            { availability.online ? 'Unable to load receipt evidence. Refresh will retry while this page is visible.' :
              'Offline. Receipt evidence will be requested when the connection returns.' }
          </Text>
        ) : null }
      </Flex>
    </>
  );
};

const PaxeerXReceiptPageContent = () => {
  const router = useRouter();
  const chain = useMultichainContext()?.chain;
  const id = getQueryParamString(router.query.id);
  throwOnAbsentParamError(id);
  return <ReceiptContent key={ `${ chain?.id ?? '' }:${ id }` } id={ id } chainId={ chain?.id }/>;
};

export default PaxeerXReceiptPageContent;
