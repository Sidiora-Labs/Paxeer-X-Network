import { chakra, Text } from '@chakra-ui/react';
import React from 'react';

import { Alert } from 'toolkit/chakra/alert';
import { Link } from 'toolkit/chakra/link';
import { apos } from 'toolkit/utils/htmlEntities';

function ChartsLoadingErrorAlert() {
  return (
    <Alert
      status="warning"
      data-charts-error
      mb={ 4 }
      closable
      borderRadius="md"
      borderWidth="1px"
      borderStyle="solid"
      borderColor="border.divider"
    >
      <Text mr={ 2 } textStyle="sm" color="text.primary">
        { `Some of the charts did not load because the server didn${ apos }t respond. To reload charts ` }
        <chakra.span>
          <Link href={ window.document.location.href }>click once again.</Link>
        </chakra.span>
      </Text>
    </Alert>
  );
}

export default ChartsLoadingErrorAlert;
