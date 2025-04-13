import React from 'react';
import { TurboModule } from 'react-native';
import { type NativeArg } from '../utils';
export type RNMBEvent<PayloadType = {
    [key: string]: string;
}> = {
    payload: PayloadType;
    type: string;
};
declare const NativeBridgeComponent: <Props extends object, BaseComponent extends new (...ags: any[]) => React.Component<Props, {}, any>>(Base: BaseComponent, turboModule: TurboModule) => {
    new (...args: any[]): {
        _turboModule: TurboModule;
        _preRefMapMethodQueue: {
            method: {
                name: string;
                args: NativeArg[];
            };
            resolver: (value: NativeArg) => void;
        }[];
        _runPendingNativeMethods<RefType>(nativeRef: RefType): Promise<void>;
        _runNativeMethod<RefType_1, ReturnType_1 = NativeArg>(methodName: string, nativeRef: RefType_1 | undefined, args?: NativeArg[]): Promise<ReturnType_1>;
        context: unknown;
        setState<K extends never>(state: {} | ((prevState: Readonly<{}>, props: Readonly<Props>) => {} | Pick<{}, K> | null) | Pick<{}, K> | null, callback?: (() => void) | undefined): void;
        forceUpdate(callback?: (() => void) | undefined): void;
        render(): React.ReactNode;
        readonly props: Readonly<Props>;
        state: Readonly<{}>;
        refs: {
            [key: string]: React.ReactInstance;
        };
        componentDidMount?(): void;
        shouldComponentUpdate?(nextProps: Readonly<Props>, nextState: Readonly<{}>, nextContext: any): boolean;
        componentWillUnmount?(): void;
        componentDidCatch?(error: Error, errorInfo: React.ErrorInfo): void;
        getSnapshotBeforeUpdate?(prevProps: Readonly<Props>, prevState: Readonly<{}>): any;
        componentDidUpdate?(prevProps: Readonly<Props>, prevState: Readonly<{}>, snapshot?: any): void;
        componentWillMount?(): void;
        UNSAFE_componentWillMount?(): void;
        componentWillReceiveProps?(nextProps: Readonly<Props>, nextContext: any): void;
        UNSAFE_componentWillReceiveProps?(nextProps: Readonly<Props>, nextContext: any): void;
        componentWillUpdate?(nextProps: Readonly<Props>, nextState: Readonly<{}>, nextContext: any): void;
        UNSAFE_componentWillUpdate?(nextProps: Readonly<Props>, nextState: Readonly<{}>, nextContext: any): void;
    };
} & BaseComponent;
export default NativeBridgeComponent;
//# sourceMappingURL=NativeBridgeComponent.d.ts.map