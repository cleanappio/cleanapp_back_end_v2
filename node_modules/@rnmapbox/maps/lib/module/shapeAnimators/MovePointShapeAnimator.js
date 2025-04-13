"use strict";

import NativeRNMBXMovePointShapeAnimatorModule from '../specs/NativeRNMBXMovePointShapeAnimatorModule';
import ShapeAnimatorManager from './ShapeAnimatorManager';
export default class MovePointShapeAnimator {
  constructor(startCoordinate) {
    const tag = ShapeAnimatorManager.nextTag();
    NativeRNMBXMovePointShapeAnimatorModule.create(tag, [startCoordinate[0], startCoordinate[1]]);
    this.__nativeTag = tag;
  }
  moveTo(args) {
    NativeRNMBXMovePointShapeAnimatorModule.moveTo(this.__nativeTag, args.coordinate, args.durationMs);
  }
}
//# sourceMappingURL=MovePointShapeAnimator.js.map