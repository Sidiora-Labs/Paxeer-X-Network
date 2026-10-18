interface StoreConfig {
  name: string;
  keyPath?: string;
  autoIncrement?: boolean;
}

interface DatabaseConfig {
  name: string;
  version: number;
  stores: StoreConfig[];
}

export const PAXEER_NETWORK_SYMBOLS = {
  name: "PAXEER_NETWORK_SYMBOLS",
  keyPath: "symbol",
  autoIncrement: false,
};

export const PAXEER_NETWORK_MAIN_CHAIN_INFO = {
  name: "PAXEER_NETWORK_MAIN_CHAIN_INFO",
  keyPath: "chain_id",
  autoIncrement: false,
};

export const PAXEER_NETWORK_TEST_CHAIN_INFO = {
  name: "PAXEER_NETWORK_TEST_CHAIN_INFO",
  keyPath: "chain_id",
  autoIncrement: false,
};

export const PAXEER_NETWORK_MAIN_TOKEN = {
  name: "PAXEER_NETWORK_MAIN_TOKEN",
  keyPath: "token",
  autoIncrement: false,
};

export const PAXEER_NETWORK_TEST_TOKEN = {
  name: "PAXEER_NETWORK_TEST_TOKEN",
  keyPath: "token",
  autoIncrement: false,
};

export const DEFAULT_DATABASE_CONFIG: DatabaseConfig = {
  name: "PAXEER_NETWORK_STORE",
  version: 4,
  stores: [
    PAXEER_NETWORK_MAIN_CHAIN_INFO,
    PAXEER_NETWORK_TEST_CHAIN_INFO,
    PAXEER_NETWORK_MAIN_TOKEN,
    PAXEER_NETWORK_SYMBOLS,
    PAXEER_NETWORK_TEST_TOKEN,
  ],
};

class IndexedDBManager {
  private static instance: IndexedDBManager;
  private connections: Map<string, IDBDatabase> = new Map();
  private databaseConfig: DatabaseConfig | null = null;
  /** Promise that resolves when database initialization is complete */
  private initializationPromise: Promise<void> | null = null;

  public static getInstance(): IndexedDBManager {
    if (!IndexedDBManager.instance) {
      IndexedDBManager.instance = new IndexedDBManager();
    }
    return IndexedDBManager.instance;
  }

  public async initializeDatabase(config: DatabaseConfig): Promise<void> {
    // If initialization is already in progress, return the existing promise
    if (this.initializationPromise) {
      return this.initializationPromise;
    }

    // Create a new initialization promise
    this.initializationPromise = this._performInitialization(config);
    return this.initializationPromise;
  }

  /**
   * Performs the actual database initialization
   * Now keeps the connection open for better performance
   */
  private async _performInitialization(config: DatabaseConfig): Promise<void> {
    this.databaseConfig = config;
    return new Promise<void>((resolve, reject) => {
      const request = indexedDB.open(config.name, config.version);

      request.onupgradeneeded = (event) => {
        const db = (event.target as IDBOpenDBRequest).result;
        config.stores.forEach((storeConfig) => {
          if (!db.objectStoreNames.contains(storeConfig.name)) {
            db.createObjectStore(storeConfig.name, {
              keyPath: storeConfig.keyPath,
              autoIncrement: storeConfig.autoIncrement ?? true,
            });
            console.warn(
              `Created store '${storeConfig.name}' in database '${config.name}'`,
            );
          }
        });
      };

      request.onsuccess = () => {
        const db = request.result;

        // Store the connection for immediate use instead of closing it
        this.connections.set(config.name, db);

        // Add event listener to handle connection cleanup when closed
        db.addEventListener("close", () => {
          this.connections.delete(config.name);
          console.warn(`Database connection closed: ${config.name}`);
        });

        console.warn(
          `Database '${config.name}' initialized and connection kept open for better performance`,
        );
        resolve();
      };

      request.onerror = () => {
        this._handleInitializationError(config.name, request.error);
        reject(request.error);
      };

      request.onblocked = () => {
        const error = new Error(
          `Database '${config.name}' is blocked by another connection`,
        );
        this._handleInitializationError(config.name, error);
        reject(error);
      };
    });
  }

  /**
   * Handles initialization errors consistently
   */
  private _handleInitializationError(dbName: string, error: unknown): void {
    console.error(`Failed to initialize database '${dbName}':`, error);
    // Reset initialization promise on error so it can be retried
    this.initializationPromise = null;
  }

  public async getConnection(
    dbName: string,
    storeName: string,
  ): Promise<IDBDatabase> {
    // Ensure database is initialized before getting connection
    await this.ensureInitialized();

    // Check if connection already exists and is still valid
    if (this.connections.has(dbName)) {
      const existingConnection = this.connections.get(dbName)!;

      // Test if connection is still valid by trying to access object stores
      try {
        if (existingConnection.objectStoreNames.contains(storeName)) {
          return existingConnection;
        }
      } catch {
        // Connection is invalid, remove it from cache
        this.connections.delete(dbName);
        console.warn(
          `Database connection '${dbName}' is no longer valid, creating new one`,
        );
      }
    }

    // Create new connection if none exists or existing one is invalid
    const db = await this.createConnection(dbName);
    this.connections.set(dbName, db);

    // Add event listener to handle connection cleanup when closed
    db.addEventListener("close", () => {
      this.connections.delete(dbName);
      console.warn(`Database connection closed: ${dbName}`);
    });

    return db;
  }

  /**
   * Ensures database is initialized, initializes if not already done
   */
  private async ensureInitialized(): Promise<void> {
    // If already initializing, wait for it to complete
    if (this.initializationPromise) {
      return this.initializationPromise;
    }

    // If no config exists, initialize with default config
    if (!this.databaseConfig) {
      return this.initializeDatabase(DEFAULT_DATABASE_CONFIG);
    }

    // If config exists but database might not be initialized, check and initialize if needed
    const isActuallyInitialized = await this._checkDatabaseExists();
    if (!isActuallyInitialized) {
      // Reset promise to allow re-initialization
      this.initializationPromise = null;
      return this.initializeDatabase(this.databaseConfig);
    }

    // Database is initialized, create resolved promise for consistency
    this.initializationPromise = Promise.resolve();
    return this.initializationPromise;
  }

  /**
   * Creates a new database connection
   */
  private async createConnection(dbName: string): Promise<IDBDatabase> {
    // This method is only called after ensureInitialized(), so databaseConfig should exist
    const config = this.databaseConfig;
    if (!config) {
      throw new Error(
        "Database not initialized. Call initializeDatabase() first.",
      );
    }

    return new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open(dbName, config.version);

      request.onsuccess = () => {
        resolve(request.result);
      };

      request.onerror = () => {
        console.error(
          `Failed to connect to database '${dbName}':`,
          request.error,
        );
        reject(request.error);
      };
    });
  }

  /**
   * Checks if database and all required stores exist
   */
  private async _checkDatabaseExists(): Promise<boolean> {
    if (!this.databaseConfig) {
      return false;
    }

    try {
      return new Promise<boolean>((resolve) => {
        const config = this.databaseConfig!; // Safe because we checked above
        const request = indexedDB.open(config.name, config.version);

        request.onsuccess = () => {
          const db = request.result;
          const config = this.databaseConfig!; // Safe because we checked above
          const allStoresExist = config.stores.every((store) =>
            db.objectStoreNames.contains(store.name),
          );
          db.close();
          resolve(allStoresExist);
        };

        request.onerror = () => resolve(false);
      });
    } catch {
      return false;
    }
  }
}

export const indexedDBManager = IndexedDBManager.getInstance();

export const initializeAppDatabase = async (
  config: DatabaseConfig,
): Promise<void> => {
  try {
    await indexedDBManager.initializeDatabase(config);
    console.warn("Application database initialized successfully");
  } catch (error) {
    console.error("Failed to initialize application database:", error);
    throw error;
  }
};

export type { IndexedDBManager, DatabaseConfig, StoreConfig };
