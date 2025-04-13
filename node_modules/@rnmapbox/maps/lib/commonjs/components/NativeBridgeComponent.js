"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _utils = require("../utils");
const NativeBridgeComponent = (Base, turboModule) => class extends Base {
  constructor(...args) {
    super(...args);
    this._turboModule = turboModule;
    this._preRefMapMethodQueue = [];
  }
  async _runPendingNativeMethods(nativeRef) {
    if (nativeRef) {
      while (this._preRefMapMethodQueue.length > 0) {
        const item = this._preRefMapMethodQueue.pop();
        if (item && item.method && item.resolver) {
          const res = await this._runNativeMethod(item.method.name, nativeRef, item.method.args);
          item.resolver(res);
        }
      }
    }
  }
  _runNativeMethod(methodName, nativeRef, args = []) {
    if (!nativeRef) {
      return new Promise(resolve => {
        this._preRefMapMethodQueue.push({
          method: {
            name: methodName,
            args
          },
          resolver: resolve
        });
      });
    }
    return (0, _utils.runNativeMethod)(this._turboModule, methodName, nativeRef, args);
  }
};
var _default = exports.default = NativeBridgeComponent;
//# sourceMappingURL=NativeBridgeComponent.js.map