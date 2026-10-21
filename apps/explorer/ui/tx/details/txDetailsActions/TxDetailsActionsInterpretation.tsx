import { Box, chakra, Flex } from '@chakra-ui/react';
import React from 'react';

import useApiQuery from 'lib/api/useApiQuery';
import { TX_INTERPRETATION } from 'stubs/txInterpretation';
import { TX_ACTIONS_BLOCK_ID } from 'ui/shared/DetailedInfo/DetailedInfoActionsWrapper';
import IconSvg from 'ui/shared/IconSvg';
import TxInterpretation from 'ui/shared/tx/interpretation/TxInterpretation';

interface Props {
  hash?: string;
  isTxDataLoading: boolean;
}

const TxDetailsActionsInterpretation = ({ hash, isTxDataLoading }: Props) => {
  const txInterpretationQuery = useApiQuery('general:tx_interpretation', {
    pathParams: { hash },
    queryOptions: {
      enabled: Boolean(hash) && !isTxDataLoading,
      placeholderData: TX_INTERPRETATION,
      refetchOnMount: false,
    },
  });

  const actions = txInterpretationQuery.data?.data.summaries;

  if (!actions || actions.length < 2) {
    return null;
  }

  const isLoading = isTxDataLoading || txInterpretationQuery.isPlaceholderData;

  return (
    <Box
      id={ TX_ACTIONS_BLOCK_ID }
      data-tx-action-card
      bg="bg.surface"
      borderWidth="1px"
      borderStyle="solid"
      borderColor="border.divider"
      borderRadius="md"
      boxShadow="card"
      px={ 4 }
      py={ 3 }
    >
      <Flex columnGap={ 3 } alignItems="flex-start">
        <IconSvg name="transactions" boxSize={ 6 } color="icon.secondary" flexShrink={ 0 } isLoading={ isLoading }/>
        <Box minW={ 0 } w="100%">
          <chakra.span
            display="block"
            textStyle="xs"
            fontWeight="600"
            textTransform="uppercase"
            color="text.secondary"
            data-label
          >
            Transaction action
          </chakra.span>
          <Flex flexDir="column" rowGap={ 2 } mt={ 1 } maxH="200px" overflowY="auto" data-content>
            { actions.map((action, index: number) => (
              <TxInterpretation
                key={ index }
                summary={ action }
                isLoading={ isLoading }
              />
            )) }
          </Flex>
        </Box>
      </Flex>
    </Box>
  );
};

export default TxDetailsActionsInterpretation;
