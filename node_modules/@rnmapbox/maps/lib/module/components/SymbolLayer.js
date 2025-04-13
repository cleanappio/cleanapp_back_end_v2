"use strict";

import React from 'react';
import { View, NativeModules } from 'react-native';
import RNMBXSymbolLayerNativeComponent from '../specs/RNMBXSymbolLayerNativeComponent';
import AbstractLayer from './AbstractLayer';
import { jsx as _jsx } from "react/jsx-runtime";
export const NATIVE_MODULE_NAME = 'RNMBXSymbolLayer';
const Mapbox = NativeModules.RNMBXModule;

// @{codepart-replace-start(LayerPropsCommon.codepart-tsx)}

// @{codepart-replace-end}

/**
 * SymbolLayer is a style layer that renders icon and text labels at points or along lines on the map.
 */
export class SymbolLayer extends AbstractLayer {
  static defaultProps = {
    sourceID: Mapbox.StyleSource.DefaultSourceID
  };
  deprecationLogged = {
    snapshot: false
  };
  _shouldSnapshot() {
    let isSnapshot = false;
    if (React.Children.count(this.baseProps.children) <= 0) {
      return isSnapshot;
    }
    React.Children.forEach(this.baseProps.children, child => {
      if (child?.type === View) {
        isSnapshot = true;
      }
    });
    if (isSnapshot && !this.deprecationLogged.snapshot) {
      console.warn('SymbolLayer: passing children for symbol layer is deprecated, please use @rnmapbox/maps Image component instead. https://github.com/rnmapbox/maps/wiki/Deprecated-SymbolLayerChildren');
      this.deprecationLogged.snapshot = true;
    }
    return isSnapshot;
  }
  render() {
    const props = {
      ...this.baseProps,
      snapshot: this._shouldSnapshot(),
      sourceLayerID: this.props.sourceLayerID
    };
    return (
      /*#__PURE__*/
      // @ts-expect-error just codegen stuff
      _jsx(RNMBXSymbolLayerNativeComponent, {
        ref: this.setNativeLayer,
        ...props,
        children: this.props.children
      })
    );
  }
}
//# sourceMappingURL=SymbolLayer.js.map