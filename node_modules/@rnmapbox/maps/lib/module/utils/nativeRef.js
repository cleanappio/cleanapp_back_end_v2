"use strict";

/**
 * Helper for useRef and requireNativeComponent:
 * @example
 * RNMBXCamera = requireNativeComponent<NativeProps>('RNMBXCamera');
 * const ref = useRef<typeof RNMBXCamera)(null) as NativeRefType<NativeProps>;
 * ...
 * <RNMBXCamera ref={ref} ... />
 */

/**
 * Helper for useRef and requireNativeComponent:
 * @example
 * RNMBXCamera = requireNativeComponent<NativeProps>('RNMBXCamera');
 * const ref = nativeRef(useRef<typeof RNMBXCamera)(null));
 * ...
 * <RNMBXCamera ref={ref} ... />
 */
export default function nativeRef(c) {
  return c;
}
//# sourceMappingURL=nativeRef.js.map