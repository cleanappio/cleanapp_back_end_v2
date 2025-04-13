"use strict";

import React, { memo, useMemo } from 'react';
import { transformStyle } from '../utils/StyleValue';
import RNMBXAtmosphereNativeComponent from '../specs/RNMBXAtmosphereNativeComponent';
import { jsx as _jsx } from "react/jsx-runtime";
export const Atmosphere = /*#__PURE__*/memo(props => {
  const baseProps = useMemo(() => {
    return {
      ...props,
      reactStyle: transformStyle(props.style),
      style: undefined
    };
  }, [props]);
  return /*#__PURE__*/_jsx(RNMBXAtmosphereNativeComponent, {
    ...baseProps
  });
});
//# sourceMappingURL=Atmosphere.js.map