export class MultiBroadcastEventBus {
  private subscribers: Map<string, Array<(data: any) => void>> = new Map();

  subscribe(eventName: string, callback: (data: any) => void): () => void {
    if (!this.subscribers.has(eventName)) {
      this.subscribers.set(eventName, []);
    }
    const callbacks = this.subscribers.get(eventName)!;
    callbacks.push(callback);

    return () => {
      this.unsubscribe(eventName, callback);
    };
  }

  unsubscribe(eventName: string, callback: (data: any) => void): void {
    if (this.subscribers.has(eventName)) {
      const callbacks = this.subscribers.get(eventName)!;
      this.subscribers.set(
        eventName,
        callbacks.filter((cb) => cb !== callback)
      );
      if (this.subscribers.get(eventName)!.length === 0) {
        this.subscribers.delete(eventName);
      }
    }
  }

  publish(eventName: string, data: any): void {
    if (this.subscribers.has(eventName)) {
      const callbacks = this.subscribers.get(eventName)!;
      callbacks.forEach((callback) => {
        callback(data);
      });
    }
  }
}
