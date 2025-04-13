import { Position } from '../types/Position';
import { ShapeAnimatorInterface } from '.';
export default class MovePointShapeAnimator implements ShapeAnimatorInterface {
    __nativeTag: number;
    constructor(startCoordinate: Position);
    moveTo(args: {
        coordinate: Position;
        durationMs: number;
    }): void;
}
//# sourceMappingURL=MovePointShapeAnimator.d.ts.map