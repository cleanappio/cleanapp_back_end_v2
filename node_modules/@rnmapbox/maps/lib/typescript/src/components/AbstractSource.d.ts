import React from 'react';
import { NativeMethods } from 'react-native';
import type { BaseProps } from '../types/BaseProps';
declare class AbstractSource<PropsType, NativePropsType extends object> extends React.PureComponent<PropsType & BaseProps> {
    _nativeRef?: React.Component<NativePropsType> & Readonly<NativeMethods>;
    setNativeProps(props: NativePropsType): void;
    setNativeRef: (instance: React.Component<NativePropsType> & Readonly<NativeMethods>) => void;
}
export default AbstractSource;
//# sourceMappingURL=AbstractSource.d.ts.map