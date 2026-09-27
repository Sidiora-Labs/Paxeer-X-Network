import { Box } from '@chakra-ui/react';
import React from 'react';

import type { Props } from './types';

import AppErrorBoundary from 'ui/shared/AppError/AppErrorBoundary';
import HeaderAlert from 'ui/snippets/header/HeaderAlert';
import HeaderMobile from 'ui/snippets/header/HeaderMobile';

import * as Layout from './components';

const LayoutHome = ({ children }: Props) => {
  return (
    <Layout.Root content={ children }>
      <Layout.Container>
        <Layout.TopRow/>
        <Layout.NavBar/>
        <HeaderMobile hideSearchButton/>
        <Layout.MainArea>
          <Layout.SideBar/>
          <Layout.MainColumn
            position="relative"
            paddingTop={{ base: 3, lg: 6 }}
          >
            <Box
              data-label="hero-band"
              position="absolute"
              top={ 0 }
              left={ 0 }
              right={ 0 }
              height={{ base: '180px', lg: '260px' }}
              bgColor="bg.surface"
              borderBottomWidth="1px"
              borderColor="border.divider"
              pointerEvents="none"
              zIndex={ 0 }
            />
            <Box position="relative" zIndex={ 1 }>
              <HeaderAlert mb={ 3 }/>
              <AppErrorBoundary>
                { children }
              </AppErrorBoundary>
            </Box>
          </Layout.MainColumn>
        </Layout.MainArea>
        <Layout.Footer/>
      </Layout.Container>
    </Layout.Root>
  );
};

export default LayoutHome;
