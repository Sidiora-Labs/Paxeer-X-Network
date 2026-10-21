import { Box, chakra, Flex } from '@chakra-ui/react';
import React from 'react';

import { Link } from 'toolkit/chakra/link';
import { Hint } from 'toolkit/components/Hint/Hint';

export interface ScanExpanderProps {
  label?: string;
  hint?: string;
  showText?: string;
  hideText?: string;
  defaultOpen?: boolean;
  children: React.ReactNode;
  className?: string;
}

const ScanExpander = ({
  label = 'More Details',
  hint,
  showText = '+ Click to show more',
  hideText = '- Click to show less',
  defaultOpen = false,
  children,
  className,
}: ScanExpanderProps) => {
  const [ isOpen, setIsOpen ] = React.useState(defaultOpen);

  const handleClick = React.useCallback(() => {
    setIsOpen((flag) => !flag);
  }, []);

  return (
    <Box
      className={ className }
      data-scan-expander
      data-open={ isOpen }
      bg="bg.surface"
      borderWidth="1px"
      borderStyle="solid"
      borderColor="border.divider"
      borderRadius="md"
      boxShadow="card"
      px={ 4 }
      py={ 3 }
    >
      <Flex alignItems="center" columnGap={ 2 } flexWrap="wrap">
        { hint && <Hint label={ hint } boxSize={ 4 }/> }
        <chakra.span textStyle="sm" color="text.secondary" data-label>{ label }:</chakra.span>
        <Link onClick={ handleClick } textStyle="sm" data-toggle>
          { isOpen ? hideText : showText }
        </Link>
      </Flex>
      { isOpen && <Box mt={ 3 } data-content>{ children }</Box> }
    </Box>
  );
};

export default React.memo(ScanExpander);
