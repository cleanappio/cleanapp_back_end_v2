import type { TurboModule } from 'react-native/Libraries/TurboModule/RCTExport';
import { Int32 } from 'react-native/Libraries/Types/CodegenTypes';
type ObjectOr<T> = Object;
type StringOr<_T> = string;
type Domain = 'Maps' | 'Navigation' | 'Search' | 'ADAS';
type Tag = Int32;
type Value = {
    value: string | number;
};
export interface Spec extends TurboModule {
    shared(path?: string): Promise<Tag>;
    setOption(tag: Tag, key: string, domain: StringOr<Domain>, value: ObjectOr<Value>): Promise<void>;
}
declare const _default: Spec;
export default _default;
//# sourceMappingURL=NativeRNMBXTileStoreModule.d.ts.map