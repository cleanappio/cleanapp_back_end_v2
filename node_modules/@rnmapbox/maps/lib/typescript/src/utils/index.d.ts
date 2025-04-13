import React from 'react';
import { ImageSourcePropType, TurboModule } from 'react-native';
export declare function isAndroid(): boolean;
export declare function existenceChange(cur: boolean, next: boolean): boolean;
export declare function isFunction(fn: unknown): fn is boolean;
export declare function isNumber(num: unknown): num is number;
export declare function isUndefined(obj: unknown): obj is undefined;
export declare function isString(str: unknown): str is string;
export declare function isBoolean(bool: unknown): bool is boolean;
export declare function isPrimitive(value: unknown): value is string | number | boolean;
export type NativeArg = string | number | boolean | null | {
    [k: string]: NativeArg;
} | NativeArg[];
export declare function runNativeMethod<ReturnType = NativeArg>(turboModule: TurboModule, name: string, nativeRef: any, args: NativeArg[]): Promise<ReturnType>;
export declare function cloneReactChildrenWithProps(children: Parameters<typeof React.Children.map>[0], propsToAdd?: {
    [key: string]: string;
}): any;
export declare function resolveImagePath(imageRef: ImageSourcePropType): string;
export declare function toJSONString(json?: any): string;
export type OrnamentPositonProp = {
    top: number;
    left: number;
} | {
    top: number;
    right: number;
} | {
    bottom: number;
    left: number;
} | {
    bottom: number;
    right: number;
};
//# sourceMappingURL=index.d.ts.map