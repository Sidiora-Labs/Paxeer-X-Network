import { Flex } from '@chakra-ui/react';
import type { UseQueryResult } from '@tanstack/react-query';
import React from 'react';

import type { TokenVerifiedInfo as TTokenVerifiedInfo } from 'types/api/token';

import config from 'configs/app';
import type { ResourceError } from 'lib/api/resources';
import { Link } from 'toolkit/chakra/link';
import { Skeleton } from 'toolkit/chakra/skeleton';
import IconSvg from 'ui/shared/IconSvg';

import TokenProjectInfo from './TokenProjectInfo';

interface Props {
  verifiedInfoQuery: UseQueryResult<TTokenVerifiedInfo, ResourceError<unknown>>;
}

const TokenVerifiedInfo = ({ verifiedInfoQuery }: Props) => {

  const { data, isPending, isError } = verifiedInfoQuery;

  const content = (() => {
    if (!config.features.verifiedTokens.isEnabled) {
      return null;
    }

    if (isPending) {
      return (
        <>
          <Skeleton loading w="100px" h="30px" borderRadius="sm"/>
          <Skeleton loading w="70px" h="30px" borderRadius="sm"/>
        </>
      );
    }

    if (isError) {
      return null;
    }

    const websiteLink = (() => {
      try {
        const url = new URL(data.projectWebsite);
        return (
          <Link
            external
            href={ data.projectWebsite }
            variant="underlaid"
            flexShrink={ 0 }
            textStyle="sm"
            display="inline-flex"
            alignItems="center"
            columnGap={ 1 }
            data-token-website
          >
            <IconSvg name="globe" boxSize={ 4 }/>
            { url.host }
          </Link>
        );
      } catch (error) {
        return null;
      }
    })();

    return (
      <>
        { websiteLink }
        <TokenProjectInfo data={ data }/>
      </>
    );
  })();

  if (!content) {
    return null;
  }

  return (
    <Flex alignItems="center" columnGap={ 2 } rowGap={ 2 } flexWrap="wrap" data-token-project>
      { content }
    </Flex>
  );
};

export default React.memo(TokenVerifiedInfo);
