export interface WalletConnector {
  connect: () => Promise<void>;
}

class BaseConnector implements WalletConnector {
  connect() {
    return Promise.resolve();
  }
}

class Blocknative implements WalletConnector {
  connect() {
    return Promise.resolve();
  }
}