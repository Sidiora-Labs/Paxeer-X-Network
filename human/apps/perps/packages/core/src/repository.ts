export interface Repository {
  save: (address: string, data: Record<string, any>) => void;
  getAll: (address: string) => Record<string, any>;
  get: (address: string, key: string) => any;
  clear: (address: string) => void;
  update: (address: string, key: string, data: any) => void;
  remove: (address: string, key: string) => void;
}

export class LocalStorageRepository implements Repository {
  constructor(private readonly name: string) {}
  /**
   * Save data to local storage. If data with the same key exists, it will be merged.
   * @param data
   */
  save(address: string, data: Record<string, any>) {
    this.run(() => {
      let all = this.getAll(address);

      all = { [address]: { ...all, ...data } };

      localStorage.setItem(this.name, JSON.stringify(all));
    });
  }
  /**
   * get data from local storage
   * @param key
   * @returns
   */
  get(address: string, key: string) {
    const data = this.getAll(address);
    if (!data) {
      return null;
    }
    return data[key];
  }
  /**
   * get all data from local storage
   * @param address
   * @returns
   */
  getAll(address: string) {
    try {
      const data = localStorage.getItem(this.name);
      if (!data) {
        return null;
      }
      return JSON.parse(data)[address];
    } catch (error) {
      console.error(error);
      return null;
    }
  }
  clear(address: string) {
    this.run(() => {
      const all = this.getAll(address);
      if (!all) {
        return;
      }
      delete all[address];
      localStorage.setItem(this.name, JSON.stringify(all));
    });
  }
  update(address: string, key: string, data: any) {
    this.run(() => {
      const all = this.getAll(address);
      all[key] = data;
      localStorage.setItem(this.name, JSON.stringify(all));
    });
  }
  remove(address: string, key: string) {
    this.run(() => {
      const all = this.getAll(address);
      delete all[key];
      localStorage.setItem(this.name, JSON.stringify(all));
    });
  }

  private run(cb: () => void) {
    if (typeof window !== "undefined" && typeof localStorage !== "undefined") {
      cb();
    }
  }
}
