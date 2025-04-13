"use strict";

import NativeRNMBXChangeLineOffsetsShapeAnimatorModule from '../specs/NativeRNMBXChangeLineOffsetsShapeAnimatorModule';
import ShapeAnimatorManager from './ShapeAnimatorManager';
export default class ChangeLineOffsetsShapeAnimator {
  constructor(args) {
    const tag = ShapeAnimatorManager.nextTag();
    NativeRNMBXChangeLineOffsetsShapeAnimatorModule.create(tag, args.coordinates, args.startOffset, args.endOffset);
    this.__nativeTag = tag;
  }
  setLineString(args) {
    NativeRNMBXChangeLineOffsetsShapeAnimatorModule.setLineString(this.__nativeTag, args.coordinates, args.startOffset ?? -1, args.endOffset ?? -1);
  }
  setStartOffset(args) {
    NativeRNMBXChangeLineOffsetsShapeAnimatorModule.setStartOffset(this.__nativeTag, args.offset, args.durationMs);
  }
  setEndOffset(args) {
    NativeRNMBXChangeLineOffsetsShapeAnimatorModule.setEndOffset(this.__nativeTag, args.offset, args.durationMs);
  }
}
//# sourceMappingURL=ChangeLineOffsetsShapeAnimator.js.map