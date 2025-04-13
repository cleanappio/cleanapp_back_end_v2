"use strict";

import { findNodeHandle } from 'react-native';
export class NativeCommands {
  constructor(module) {
    this.module = module;
    this.preRefMethodQueue = [];
  }
  async setNativeRef(nativeRef) {
    if (nativeRef) {
      this.nativeRef = nativeRef;
      while (this.preRefMethodQueue.length > 0) {
        const item = this.preRefMethodQueue.pop();
        if (item && item.method && item.resolver) {
          const res = await this._call(item.method.name, nativeRef, item.method.args);
          item.resolver(res);
        }
      }
    }
  }
  call(name, args) {
    if (this.nativeRef) {
      return this._call(name, this.nativeRef, args);
    } else {
      return new Promise(resolve => {
        this.preRefMethodQueue.push({
          method: {
            name,
            args
          },
          resolver: resolve
        });
      });
    }
  }
  _call(name, nativeRef, args) {
    const handle = findNodeHandle(nativeRef);
    if (handle) {
      return this.module[name](handle, ...args);
    } else {
      throw new Error(`Could not find handle for native ref ${module} when trying to invoke ${String(name)}`);
    }
  }
}
//# sourceMappingURL=NativeCommands.js.map