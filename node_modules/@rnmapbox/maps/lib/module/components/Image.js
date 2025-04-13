"use strict";

import React, { memo, forwardRef } from 'react';
import { findNodeHandle } from 'react-native';
import RNMBXImageNativeComponent from '../specs/RNMBXImageNativeComponent';
import NativeRNMBXImageModule from '../specs/NativeRNMBXImageModule';
import { jsx as _jsx } from "react/jsx-runtime";
const Image = /*#__PURE__*/memo(/*#__PURE__*/forwardRef(function Image({
  name,
  sdf,
  stretchX,
  stretchY,
  children
}, ref) {
  const nativeProps = {
    name,
    sdf,
    stretchX,
    stretchY,
    children
  };
  const imageRef = React.useRef(null);
  const refresh = () => {
    const handle = findNodeHandle(imageRef.current);
    NativeRNMBXImageModule.refresh(handle);
  };
  React.useImperativeHandle(ref, () => {
    return {
      refresh
    };
  });

  // @ts-expect-error just codegen stuff
  return /*#__PURE__*/_jsx(RNMBXImageNativeComponent, {
    ...nativeProps,
    ref: imageRef
  });
}));
Image.displayName = 'Image';
export default Image;
//# sourceMappingURL=Image.js.map