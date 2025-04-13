import { Position } from '@turf/helpers';
import { ShapeAnimatorInterface } from '.';
export default class ChangeLineOffsetsShapeAnimator implements ShapeAnimatorInterface {
    __nativeTag: number;
    constructor(args: {
        coordinates: Position[];
        startOffset: number;
        endOffset: number;
    });
    setLineString(args: {
        coordinates: Position[];
        startOffset?: number;
        endOffset?: number;
    }): void;
    setStartOffset(args: {
        offset: number;
        durationMs: number;
    }): void;
    setEndOffset(args: {
        offset: number;
        durationMs: number;
    }): void;
}
//# sourceMappingURL=ChangeLineOffsetsShapeAnimator.d.ts.map