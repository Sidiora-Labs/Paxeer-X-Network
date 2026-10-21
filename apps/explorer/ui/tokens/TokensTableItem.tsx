import { chakra, Flex } from '@chakra-ui/react';
import BigNumber from 'bignumber.js';
import React from 'react';

import type { TokenInfo } from 'types/api/token';
import type { AggregatedTokenInfo } from 'types/client/multichainAggregator';

import config from 'configs/app';
import multichainConfig from 'configs/multichain';
import getItemIndex from 'lib/getItemIndex';
import { getTokenTypeName } from 'lib/token/tokenTypes';
import { currencyUnits } from 'lib/units';
import { Skeleton } from 'toolkit/chakra/skeleton';
import { TableCell, TableRow } from 'toolkit/chakra/table';
import { Tag } from 'toolkit/chakra/tag';
import AddressAddToWallet from 'ui/shared/address/AddressAddToWallet';
import TokenEntity from 'ui/shared/entities/token/TokenEntity';
import IconSvg from 'ui/shared/IconSvg';
import SimpleValue from 'ui/shared/value/SimpleValue';
import { DEFAULT_ACCURACY_USD } from 'ui/shared/value/utils';

export type TokensTableToken = TokenInfo | AggregatedTokenInfo;

// The token list carries no figure for every column the scan layout shows, and a figure the payload
// never sent reads as this dash rather than as a number nobody measured.
export const TOKEN_VALUE_PLACEHOLDER = '–';

const PRICE_ACCURACY = 4;
const NATIVE_PRICE_ACCURACY = 6;

// The onchain capitalisation is the supply the contract reports taken at the price the list carries,
// which is the capitalisation the chain itself can answer for.
export function getOnchainMarketCap(token: TokensTableToken): BigNumber | undefined {
  const { total_supply: totalSupply, decimals, exchange_rate: exchangeRate } = token;

  if (!totalSupply || !exchangeRate) {
    return undefined;
  }

  const supply = BigNumber(totalSupply).div(BigNumber(10).pow(Number(decimals) || 0));
  const marketCap = supply.multipliedBy(BigNumber(exchangeRate));

  return marketCap.isFinite() ? marketCap : undefined;
}

// The price beneath the fiat price is the same price denominated in the network coin: the fiat price
// of the token over the fiat price of the coin.
export function getNativePrice(exchangeRate: string | null, coinPrice: string | null | undefined): BigNumber | undefined {
  if (!exchangeRate || !coinPrice) {
    return undefined;
  }

  const price = BigNumber(coinPrice);

  if (price.isZero() || !price.isFinite()) {
    return undefined;
  }

  const nativePrice = BigNumber(exchangeRate).div(price);

  return nativePrice.isFinite() ? nativePrice : undefined;
}

export const TokenValuePlaceholder = ({ isLoading }: { isLoading?: boolean }) => (
  <Skeleton loading={ isLoading } display="inline-block" color="text.secondary" data-token-value-placeholder>
    <span>{ TOKEN_VALUE_PLACEHOLDER }</span>
  </Skeleton>
);

interface TokenChangePercentProps {
  value?: number | null;
  isLoading?: boolean;
}

export const TokenChangePercent = ({ value, isLoading }: TokenChangePercentProps) => {
  if (value === undefined || value === null || !Number.isFinite(value)) {
    return <TokenValuePlaceholder isLoading={ isLoading }/>;
  }

  const isUp = value >= 0;

  return (
    <Skeleton
      loading={ isLoading }
      display="inline-flex"
      alignItems="center"
      columnGap={ 1 }
      color={ isUp ? 'feedback.success.fg' : 'feedback.error.fg' }
      data-token-change={ isUp ? 'up' : 'down' }
    >
      <IconSvg name="arrows/up-head" boxSize={ 3 } transform={ isUp ? undefined : 'rotate(180deg)' }/>
      <span>{ `${ Math.abs(value).toFixed(2) }%` }</span>
    </Skeleton>
  );
};

interface TokenFiatValueProps {
  value?: BigNumber;
  isLoading?: boolean;
  accuracy?: number;
}

export const TokenFiatValue = ({ value, isLoading, accuracy = DEFAULT_ACCURACY_USD }: TokenFiatValueProps) => {
  if (!value) {
    return <TokenValuePlaceholder isLoading={ isLoading }/>;
  }

  return <SimpleValue value={ value } loading={ isLoading } prefix="$" accuracy={ accuracy }/>;
};

type Props = {
  token: TokensTableToken;
  index: number;
  page: number;
  isLoading?: boolean;
  coinPrice?: string | null;
};

const bridgedTokensFeature = config.features.bridgedTokens;

const TokensTableItem = ({
  token,
  page,
  index,
  isLoading,
  coinPrice,
}: Props) => {

  const {
    exchange_rate: exchangeRate,
    type,
    holders_count: holdersCount,
    circulating_market_cap: marketCap,
  } = token;

  const originalChainId = 'origin_chain_id' in token ? token.origin_chain_id : undefined;
  const chainInfos = 'chain_infos' in token ? token.chain_infos : undefined;

  const bridgedChainTag = bridgedTokensFeature.isEnabled ?
    bridgedTokensFeature.chains.find(({ id }) => id === originalChainId)?.short_title :
    undefined;

  const chainInfo = React.useMemo(() => {
    if (!chainInfos) {
      return;
    }

    const chainId = Object.keys(chainInfos)[0];
    const chain = multichainConfig()?.chains.find((chain) => chain.id === chainId);
    return chain;
  }, [ chainInfos ]);

  const nativePrice = getNativePrice(exchangeRate, coinPrice);
  const onchainMarketCap = getOnchainMarketCap(token);

  return (
    <TableRow className="group" data-token-row>
      <TableCell>
        <Skeleton loading={ isLoading } textStyle="sm" color="text.secondary" display="inline-block" minW={ 5 } data-token-rank>
          { getItemIndex(index, page) }
        </Skeleton>
      </TableCell>
      <TableCell>
        <Flex alignItems="center" columnGap={ 2 } overflow="hidden">
          <TokenEntity
            token={ token }
            chain={ chainInfo }
            isLoading={ isLoading }
            jointSymbol
            noCopy
            textStyle="sm"
            fontWeight="600"
            noLink={ type === 'NATIVE' }
          />
          <Flex flexShrink={ 0 } columnGap={ 1 } alignItems="center">
            <Tag loading={ isLoading } variant="outlined">{ getTokenTypeName(type, chainInfo?.app_config) }</Tag>
            { bridgedChainTag && <Tag loading={ isLoading } variant="outlined">{ bridgedChainTag }</Tag> }
            { type !== 'NATIVE' && (
              <AddressAddToWallet
                token={ token }
                isLoading={ isLoading }
                iconSize={ 5 }
                opacity={ 0 }
                _groupHover={{ opacity: 1 }}
                chainConfig={ chainInfo?.app_config }
              />
            ) }
          </Flex>
        </Flex>
      </TableCell>
      <TableCell isNumeric>
        <Flex flexDir="column" alignItems="flex-end" rowGap={ 1 } data-token-price>
          <TokenFiatValue
            value={ exchangeRate ? BigNumber(exchangeRate) : undefined }
            accuracy={ PRICE_ACCURACY }
            isLoading={ isLoading }
          />
          { nativePrice && (
            <Skeleton loading={ isLoading } textStyle="xs" color="text.secondary" data-token-native-price>
              <chakra.span>{ `${ nativePrice.dp(NATIVE_PRICE_ACCURACY).toFormat() } ${ currencyUnits.ether }` }</chakra.span>
            </Skeleton>
          ) }
        </Flex>
      </TableCell>
      <TableCell isNumeric>
        <TokenChangePercent isLoading={ isLoading }/>
      </TableCell>
      <TableCell isNumeric>
        <TokenValuePlaceholder isLoading={ isLoading }/>
      </TableCell>
      <TableCell isNumeric>
        <TokenFiatValue value={ marketCap ? BigNumber(marketCap) : undefined } isLoading={ isLoading }/>
      </TableCell>
      <TableCell isNumeric>
        <TokenFiatValue value={ onchainMarketCap } isLoading={ isLoading }/>
      </TableCell>
      <TableCell isNumeric>
        <Skeleton loading={ isLoading } display="inline-block" data-token-holders>
          { Number(holdersCount ?? 0).toLocaleString() }
        </Skeleton>
      </TableCell>
    </TableRow>
  );
};

export default React.memo(TokensTableItem);
