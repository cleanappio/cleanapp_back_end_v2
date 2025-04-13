"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _NativeRNMBXChangeLineOffsetsShapeAnimatorModule = _interopRequireDefault(require("../specs/NativeRNMBXChangeLineOffsetsShapeAnimatorModule"));
var _ShapeAnimatorManager = _interopRequireDefault(require("./ShapeAnimatorManager"));
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
class ChangeLineOffsetsShapeAnimator {
  constructor(args) {
    const tag = _ShapeAnimatorManager.default.nextTag();
    _NativeRNMBXChangeLineOffsetsShapeAnimatorModule.default.create(tag, args.coordinates, args.startOffset, args.endOffset);
    this.__nativeTag = tag;
  }
  setLineString(args) {
    _NativeRNMBXChangeLineOffsetsShapeAnimatorModule.default.setLineString(this.__nativeTag, args.coordinates, args.startOffset ?? -1, args.endOffset ?? -1);
  }
  setStartOffset(args) {
    _NativeRNMBXChangeLineOffsetsShapeAnimatorModule.default.setStartOffset(this.__nativeTag, args.offset, args.durationMs);
  }
  setEndOffset(args) {
    _NativeRNMBXChangeLineOffsetsShapeAnimatorModule.default.setEndOffset(this.__nativeTag, args.offset, args.durationMs);
  }
}
exports.default = ChangeLineOffsetsShapeAnimator;
//# sourceMappingURL=ChangeLineOffsetsShapeAnimator.js.map