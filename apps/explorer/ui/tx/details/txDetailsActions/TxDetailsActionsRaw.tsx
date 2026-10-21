import { Box, chakra, Flex } from '@chakra-ui/react';
import React from 'react';

import type { TxAction } from 'types/api/txAction';

import { TX_ACTIONS_BLOCK_ID } from 'ui/shared/DetailedInfo/DetailedInfoActionsWrapper';
import IconSvg from 'ui/shared/IconSvg';

import TxDetailsAction from './TxDetailsAction';

interface Props {
  actions: Array<TxAction>;
  isLoading: boolean;
}

const TxDetailsActionsRaw = ({ actions, isLoading }: Props) => {
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
            { actions.map((action, index: number) => <TxDetailsAction key={ index } action={ action }/>) }
          </Flex>
        </Box>
      </Flex>
    </Box>
  );
};

export default TxDetailsActionsRaw;
