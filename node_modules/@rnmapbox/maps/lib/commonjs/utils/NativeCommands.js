"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.NativeCommands = void 0;
var _reactNative = require("react-native");
class NativeCommands {
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
    const handle = (0, _reactNative.findNodeHandle)(nativeRef);
    if (handle) {
      return this.module[name](handle, ...args);
    } else {
      throw new Error(`Could not find handle for native ref ${module} when trying to invoke ${String(name)}`);
    }
  }
}
exports.NativeCommands = NativeCommands;
//# sourceMappingURL=NativeCommands.js.map