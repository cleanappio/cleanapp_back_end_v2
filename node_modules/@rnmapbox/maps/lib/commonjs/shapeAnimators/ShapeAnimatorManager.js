"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
class ShapeAnimatorManager {
  static tag = 42;
  static nextTag() {
    this.tag += 1;
    return this.tag;
  }
}
exports.default = ShapeAnimatorManager;
//# sourceMappingURL=ShapeAnimatorManager.js.map