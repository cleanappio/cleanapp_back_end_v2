import type { TurboModule } from 'react-native/Libraries/TurboModule/RCTExport';
import { Int32, Double } from 'react-native/Libraries/Types/CodegenTypes';
type AnimatorTag = Int32;
export interface Spec extends TurboModule {
    create(tag: AnimatorTag, coordinate: ReadonlyArray<Double>): Promise<void>;
    moveTo(tag: AnimatorTag, coordinate: ReadonlyArray<Double>, duration: Double): Promise<void>;
}
declare const _default: Spec;
export default _default;
//# sourceMappingURL=NativeRNMBXMovePointShapeAnimatorModule.d.ts.map