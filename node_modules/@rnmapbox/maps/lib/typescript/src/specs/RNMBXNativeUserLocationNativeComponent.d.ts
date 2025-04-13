import type { HostComponent, ProcessedColorValue, ViewProps } from 'react-native';
import type { Expression } from '../utils/MapboxStyles';
import { UnsafeMixed } from './codegenUtils';
type Value<T> = T | Expression;
type OptionalProp<T> = UnsafeMixed<T>;
type Pulsing = {
    isEnabled?: boolean;
    radius?: 'accuracy' | number;
    color?: ProcessedColorValue | null | undefined;
} | {
    kind: 'default';
};
export interface NativeProps extends ViewProps {
    androidRenderMode?: OptionalProp<string>;
    puckBearing?: OptionalProp<'heading' | 'course'>;
    puckBearingEnabled?: OptionalProp<boolean>;
    bearingImage?: OptionalProp<string>;
    shadowImage?: OptionalProp<string>;
    topImage?: OptionalProp<string>;
    scale?: UnsafeMixed<Value<number>>;
    visible?: boolean;
    pulsing?: UnsafeMixed<Pulsing>;
}
declare const _default: HostComponent<NativeProps>;
export default _default;
//# sourceMappingURL=RNMBXNativeUserLocationNativeComponent.d.ts.map