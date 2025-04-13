import type { HostComponent, ViewProps } from 'react-native';
import { UnsafeMixed } from './codegenUtils';
type Asset = {
    __packager_asset?: boolean;
    uri?: string;
    url?: string;
};
export interface NativeProps extends ViewProps {
    models: UnsafeMixed<{
        [key: string]: Asset;
    }>;
}
declare const _default: HostComponent<NativeProps>;
export default _default;
//# sourceMappingURL=RNMBXModelsNativeComponent.d.ts.map