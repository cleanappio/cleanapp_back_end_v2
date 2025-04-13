"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _NativeRNMBXMovePointShapeAnimatorModule = _interopRequireDefault(require("../specs/NativeRNMBXMovePointShapeAnimatorModule"));
var _ShapeAnimatorManager = _interopRequireDefault(require("./ShapeAnimatorManager"));
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
class MovePointShapeAnimator {
  constructor(startCoordinate) {
    const tag = _ShapeAnimatorManager.default.nextTag();
    _NativeRNMBXMovePointShapeAnimatorModule.default.create(tag, [startCoordinate[0], startCoordinate[1]]);
    this.__nativeTag = tag;
  }
  moveTo(args) {
    _NativeRNMBXMovePointShapeAnimatorModule.default.moveTo(this.__nativeTag, args.coordinate, args.durationMs);
  }
}
exports.default = MovePointShapeAnimator;
//# sourceMappingURL=MovePointShapeAnimator.js.map