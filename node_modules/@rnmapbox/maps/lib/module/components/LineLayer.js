"use strict";

import React from 'react';
import { NativeModules } from 'react-native';
import RNMBXLineLayerNativeComponent from '../specs/RNMBXLineLayerNativeComponent';
import AbstractLayer from './AbstractLayer';
import { jsx as _jsx } from "react/jsx-runtime";
const Mapbox = NativeModules.RNMBXModule;

// @{codepart-replace-start(LayerPropsCommon.codepart-tsx)}

// @{codepart-replace-end}

/**
 * LineLayer is a style layer that renders one or more stroked polylines on the map.
 */
class LineLayer extends AbstractLayer {
  static defaultProps = {
    sourceID: Mapbox.StyleSource.DefaultSourceID
  };
  render() {
    const props = {
      ...this.baseProps,
      sourceLayerID: this.props.sourceLayerID
    };
    return (
      /*#__PURE__*/
      // @ts-expect-error just codegen stuff
      _jsx(RNMBXLineLayerNativeComponent, {
        ref: this.setNativeLayer,
        ...props
      })
    );
  }
}
export default LineLayer;
//# sourceMappingURL=LineLayer.js.map